package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

type immersiveDestinationItem struct {
	DestinationID string                      `json:"destinationId"`
	Kind          string                      `json:"kind"`
	Name          string                      `json:"name"`
	GameCount     int64                       `json:"gameCount"`
	FeaturedGames []immersiveFeaturedGameItem `json:"featuredGames"`
}

type immersiveDestinationResponse struct {
	Items []immersiveDestinationItem `json:"items"`
}

type immersiveLibraryResponse struct {
	Library immersiveDestinationItem `json:"library"`
	Folder  *struct {
		FolderID string `json:"folderId"`
	} `json:"folder"`
	Folders []struct {
		FolderID string `json:"folderId"`
		Name     string `json:"name"`
	} `json:"folders"`
	Items []struct {
		GameID       string `json:"gameId"`
		Title        string `json:"title"`
		TitleInitial string `json:"titleInitial"`
		Favorited    bool   `json:"favorited"`
		SaveStates   []struct {
			SaveStateID   string  `json:"saveStateId"`
			ScreenshotURL *string `json:"screenshotUrl"`
		} `json:"saveStates"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func seedImmersiveFavoriteAndSave(
	t *testing.T,
	server *Server,
	profileID, favoriteGameID, savedGameID, folderID, saveStateID string,
) {
	t.Helper()
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	mustExecHTTPTest(t, transaction, `
INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,7000)
`, profileID, favoriteGameID)
	mustExecHTTPTest(t, transaction, `
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
VALUES(?,?,'待通关','待通关',1,7000,7000)
`, folderID, profileID)
	mustExecHTTPTest(t, transaction, `
INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms)
VALUES(?,?,?,7000)
`, profileID, folderID, favoriteGameID)
	statePayload := []byte("state")
	stateBlobID := seedImmersiveBlob(t, server, transaction, string(statePayload), "application/octet-stream", 7000)
	screenshotBlobID := seedImmersiveBlob(t, server, transaction, "screenshot", "image/png", 7000)
	var contentID, revisionID, artifactID, launchID string
	err = transaction.QueryRowContext(t.Context(), `
SELECT revision.game_content_revision_id,revision.id,revision.core_artifact_id,launch.id
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN launch_sessions launch ON launch.game_variant_revision_id=revision.id
WHERE variant.game_id=?
ORDER BY launch.created_at_ms DESC LIMIT 1
`, savedGameID).Scan(&contentID, &revisionID, &artifactID, &launchID)
	testassert.False(t, err != nil, err)
	payloadDigest := sha256.Sum256(statePayload)
	mustExecHTTPTest(t, transaction, `
INSERT INTO save_states(
 id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,core_artifact_id,
 adapter_abi,save_abi,dependency_snapshot_sha256,dat_version_id,dos_entry_path,payload_blob_id,payload_kind,
 payload_sha256,payload_size_bytes,screenshot_blob_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,
 deleted_at_ms,source_launch_session_id,disc_index
) VALUES(?,?,?,?,?,?,'emulatorjs-state-v1','emulatorjs-state-v1',?,NULL,NULL,?,'RUNTIME_STATE',?, ?,?,'第一章',100,1,7000,7000,NULL,?,NULL)
`, saveStateID, profileID, savedGameID, contentID, revisionID, artifactID, strings.Repeat("d", 64),
		stateBlobID, hex.EncodeToString(payloadDigest[:]), len(statePayload), screenshotBlobID, launchID)
	mustCommitHTTPTest(t, transaction)
}

func assertImmersiveDestinations(t *testing.T, server *Server) {
	t.Helper()
	response := immersiveGET(t, server, "/api/v1/immersive/destinations")
	result := decodeImmersiveResponse[immersiveDestinationResponse](t, response)
	testassert.Falsef(t, response.Code != http.StatusOK, "destinations = %d %s", response.Code, response.Body.String())
	testassert.Falsef(t, len(result.Items) < 5, "destinations = %#v", result)
	expectedKinds := []string{"all", "recent", "favorites", "saves"}
	for index, kind := range expectedKinds {
		testassert.Falsef(t, result.Items[index].Kind != kind,
			"destination %d = %#v", index, result.Items[index])
	}
	testassert.Falsef(t, result.Items[0].GameCount != 3, "all count = %#v", result.Items[0])
	testassert.Falsef(t, result.Items[2].GameCount != 1, "favorite count = %#v", result.Items[2])
	testassert.Falsef(t, result.Items[3].GameCount != 1, "save count = %#v", result.Items[3])
}

func assertImmersiveSortedLibraries(t *testing.T, server *Server, recentGameID string) {
	t.Helper()
	firstResponse := immersiveGET(t, server, "/api/v1/immersive/libraries/all/games?limit=2")
	first := decodeImmersiveResponse[immersiveLibraryResponse](t, firstResponse)
	testassert.Falsef(t, firstResponse.Code != http.StatusOK, "all first = %d %s", firstResponse.Code, firstResponse.Body.String())
	testassert.Falsef(t, len(first.Items) != 2, "all first items = %#v", first.Items)
	testassert.Falsef(t, first.Items[0].Title != "1994" || first.Items[0].TitleInitial != "1",
		"numeric title = %#v", first.Items[0])
	testassert.Falsef(t, first.Items[1].Title != "alpha" || first.Items[1].TitleInitial != "A",
		"ASCII title = %#v", first.Items[1])
	testassert.Falsef(t, first.NextCursor == nil, "all first cursor = %#v", first)
	secondResponse := immersiveGET(t, server, "/api/v1/immersive/libraries/all/games?limit=2&cursor="+
		url.QueryEscape(*first.NextCursor))
	second := decodeImmersiveResponse[immersiveLibraryResponse](t, secondResponse)
	testassert.Falsef(t, secondResponse.Code != http.StatusOK, "all second = %d %s", secondResponse.Code, secondResponse.Body.String())
	testassert.Falsef(t, len(second.Items) != 1, "all second items = %#v", second.Items)
	testassert.Falsef(t, second.Items[0].Title != "我的世界" || second.Items[0].TitleInitial != "W",
		"Chinese title = %#v", second.Items[0])
	recent := decodeImmersiveResponse[immersiveLibraryResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/libraries/recent/games"))
	testassert.Falsef(t, len(recent.Items) != 3, "recent games = %#v", recent.Items)
	testassert.Falsef(t, recent.Items[0].GameID != recentGameID, "recent order = %#v", recent.Items)
}

func assertImmersiveFavoriteAndSaveLibraries(
	t *testing.T,
	server *Server,
	folderID, saveStateID string,
) {
	t.Helper()
	favorites := decodeImmersiveResponse[immersiveLibraryResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/libraries/favorites/games"))
	testassert.Falsef(t, len(favorites.Folders) != 1, "favorite folders = %#v", favorites)
	testassert.Falsef(t, favorites.Folders[0].FolderID != folderID, "favorite folder = %#v", favorites.Folders[0])
	testassert.Falsef(t, len(favorites.Items) != 1, "favorite items = %#v", favorites.Items)
	testassert.Falsef(t, !favorites.Items[0].Favorited, "favorite item = %#v", favorites.Items[0])
	folder := decodeImmersiveResponse[immersiveLibraryResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/libraries/favorites/games?folderId="+folderID))
	testassert.Falsef(t, folder.Folder == nil, "favorite folder page = %#v", folder)
	testassert.Falsef(t, folder.Folder.FolderID != folderID, "favorite folder page = %#v", folder)
	testassert.Falsef(t, len(folder.Items) != 1, "favorite folder items = %#v", folder.Items)
	saves := decodeImmersiveResponse[immersiveLibraryResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/libraries/saves/games"))
	testassert.Falsef(t, len(saves.Items) != 1, "save library = %#v", saves)
	testassert.Falsef(t, len(saves.Items[0].SaveStates) != 1, "save states = %#v", saves.Items[0])
	actualSave := saves.Items[0].SaveStates[0]
	testassert.Falsef(t, actualSave.SaveStateID != saveStateID, "save = %#v", actualSave)
	testassert.Falsef(t, actualSave.ScreenshotURL == nil || *actualSave.ScreenshotURL != "/content/save-states/"+saveStateID+"/screenshot",
		"save screenshot = %#v", actualSave)
	mustExecHTTPTest(t, server.database, "UPDATE save_states SET screenshot_blob_id=NULL WHERE id=?", saveStateID)
	withoutScreenshot := decodeImmersiveResponse[immersiveLibraryResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/libraries/saves/games"))
	testassert.Falsef(t, withoutScreenshot.Items[0].SaveStates[0].ScreenshotURL != nil,
		"screenshot-less immersive save = %#v", withoutScreenshot.Items[0].SaveStates[0])
	invalidFolder := immersiveGET(t, server, "/api/v1/immersive/libraries/all/games?folderId="+folderID)
	testassert.Falsef(t, invalidFolder.Code != http.StatusNotFound,
		"invalid library folder = %d %s", invalidFolder.Code, invalidFolder.Body.String())
}

func TestImmersiveDestinationsAndProfileLibraries(t *testing.T) {
	server := newTestServer(t)
	profileID := "01980000-0000-7000-8000-00000000f101"
	mustExecHTTPTest(t, server.database, "INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Player',0)", profileID)
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: "01980000-0000-7000-8000-00000000f102", ProfileID: profileID,
		Username: "player", DisplayName: "Player", Role: "USER",
	}}
	seeds := []immersiveGameSeed{
		{
			GameID: "01980000-0000-7000-8000-00000000ac01", MetadataID: "01980000-0000-7000-8000-00000000bc01",
			ContentID: "01980000-0000-7000-8000-00000000cc01", Title: "我的世界", Description: "World",
			CoverID: "01980000-0000-7000-8000-00000000dc01",
		},
		{
			GameID: "01980000-0000-7000-8000-00000000ac02", MetadataID: "01980000-0000-7000-8000-00000000bc02",
			ContentID: "01980000-0000-7000-8000-00000000cc02", Title: "1994", Description: "Number",
		},
		{
			GameID: "01980000-0000-7000-8000-00000000ac03", MetadataID: "01980000-0000-7000-8000-00000000bc03",
			ContentID: "01980000-0000-7000-8000-00000000cc03", Title: "alpha", Description: "Alpha",
		},
	}
	for index, seed := range seeds {
		seedImmersiveGame(t, server, seed, int64(1000+index))
		seedImmersivePlay(t, server, seed, profileID, int64(4000+index*1000), int64(201+index))
	}
	folderID := "01980000-0000-7000-8000-00000000fc01"
	saveStateID := "01980000-0000-7000-8000-00000000ec01"
	seedImmersiveFavoriteAndSave(
		t, server, profileID, seeds[2].GameID, seeds[0].GameID, folderID, saveStateID,
	)

	assertImmersiveDestinations(t, server)
	assertImmersiveSortedLibraries(t, server, seeds[2].GameID)
	assertImmersiveFavoriteAndSaveLibraries(t, server, folderID, saveStateID)
}

func TestImmersiveLibraryQueryAllowlist(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{
		"/api/v1/immersive/destinations?unknown=1",
		"/api/v1/immersive/libraries/all/games?unknown=1",
	} {
		response := immersiveGET(t, server, path)
		testassert.Falsef(t, response.Code != http.StatusBadRequest,
			"unknown query %s = %d %s", path, response.Code, response.Body.String())
	}
}
