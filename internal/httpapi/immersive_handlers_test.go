package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/gametitle"
	"retrom/internal/httpapi/generated"
	"retrom/internal/payloadrelease"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type immersiveGameSeed struct {
	GameID      string
	MetadataID  string
	ContentID   string
	Title       string
	Description string
	CoverID     string
	VideoID     string
}

type immersiveFeaturedGameItem struct {
	GameID         string  `json:"gameId"`
	Title          string  `json:"title"`
	CoverURL       *string `json:"coverUrl"`
	LastPlayedAtMS *int64  `json:"lastPlayedAtMs"`
}

type immersivePlatformItem struct {
	PlatformID     string                      `json:"platformId"`
	PlatformName   string                      `json:"platformName"`
	GameCount      int64                       `json:"gameCount"`
	LastPlayedAtMS *int64                      `json:"lastPlayedAtMs"`
	FeaturedGames  []immersiveFeaturedGameItem `json:"featuredGames"`
}

type immersivePlatformResponse struct {
	GeneratedAtMS int64                   `json:"generatedAtMs"`
	Items         []immersivePlatformItem `json:"items"`
}

type immersiveGameResponse struct {
	Platform struct {
		PlatformID     string `json:"platformId"`
		GameCount      int64  `json:"gameCount"`
		LastPlayedAtMS *int64 `json:"lastPlayedAtMs"`
	} `json:"platform"`
	Items []struct {
		GameID           string  `json:"gameId"`
		Title            string  `json:"title"`
		Description      string  `json:"description"`
		CoverURL         *string `json:"coverUrl"`
		VideoURL         *string `json:"videoUrl"`
		LastPlayedAtMS   *int64  `json:"lastPlayedAtMs"`
		PlatformInstance struct {
			ID string `json:"id"`
		} `json:"platformInstance"`
		DefaultCore struct {
			ID string `json:"id"`
		} `json:"defaultCore"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func immersiveGET(t *testing.T, server *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		response,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil),
	)
	return response
}

func decodeImmersiveResponse[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode immersive response: %v, body=%s", err, response.Body.String())
	}
	return result
}

func findImmersivePlatform(items []immersivePlatformItem, platformID string) *immersivePlatformItem {
	for index := range items {
		if items[index].PlatformID == platformID {
			return &items[index]
		}
	}
	return nil
}

func assertImmersiveFeaturedGameOrder(
	t *testing.T,
	actual []immersiveFeaturedGameItem,
	expected ...string,
) {
	t.Helper()
	testassert.Falsef(t, len(actual) != len(expected), "featured games = %#v, expected IDs = %v", actual, expected)
	for index, gameID := range expected {
		testassert.Falsef(t, actual[index].GameID != gameID,
			"featured game %d = %#v, expected ID = %s", index, actual[index], gameID)
	}
}

func assertImmersiveOperation(
	t *testing.T,
	specification *openapi3.T,
	path, operationID string,
) {
	t.Helper()
	item := specification.Paths.Value(path)
	testassert.Falsef(t, item == nil || item.Get == nil, "missing GET operation for %s", path)
	testassert.Falsef(t, item.Get.OperationID != operationID,
		"operation ID for %s = %q", path, item.Get.OperationID)
}

func assertImmersiveProviderContract(t *testing.T, specification *openapi3.T) {
	t.Helper()
	profileRef := specification.Components.Schemas["RuntimeTargetAdminItem"]
	testassert.False(t, profileRef == nil || profileRef.Value == nil, "missing RuntimeTargetAdminItem")
	profile := profileRef.Value
	for _, property := range []string{"providerId", "targetId", "bundleSha256"} {
		testassert.Falsef(t, profile.Properties[property] == nil,
			"RuntimeTargetAdminItem missing %s", property)
	}
	for _, forbidden := range []string{"playerAdapterId", "runtimeFamily", "routeKey", "targetContractSha256"} {
		testassert.Falsef(t, profile.Properties[forbidden] != nil,
			"RuntimeTargetAdminItem retains %s", forbidden)
	}
}

func TestImmersiveOpenAPIContractIsTypedAndBounded(t *testing.T) {
	t.Parallel()
	specification, err := generated.GetSpec()
	testassert.False(t, err != nil, err)
	assertImmersiveOperation(t, specification, "/api/v1/immersive/platforms", "GetImmersivePlatforms")
	assertImmersiveOperation(t, specification, "/api/v1/immersive/platforms/{platformId}/games",
		"GetImmersivePlatformGames")
	assertImmersiveOperation(t, specification, "/api/v1/immersive/destinations", "GetImmersiveDestinations")
	assertImmersiveOperation(t, specification, "/api/v1/immersive/libraries/{libraryKind}/games",
		"GetImmersiveLibraryGames")
	limit := specification.Components.Parameters["Limit50"].Value.Schema.Value
	testassert.Falsef(t, limit.Max == nil || *limit.Max != 50 || limit.Min == nil || *limit.Min != 1,
		"immersive limit schema = %#v", limit)
	assertImmersiveProviderContract(t, specification)
}

func seedImmersiveBlob(
	t *testing.T,
	server *Server,
	transaction *sql.Tx,
	payload, mediaType string,
	now int64,
) string {
	t.Helper()
	metadata, err := server.blobs.Put(bytes.NewReader([]byte(payload)))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(t.Context(), transaction, metadata, mediaType, now)
	testassert.False(t, err != nil, err)
	return blobID
}

func seedImmersiveAssets(
	t *testing.T,
	server *Server,
	transaction *sql.Tx,
	seed immersiveGameSeed,
	coverPayload, videoPayload string,
	now int64,
) {
	t.Helper()
	if seed.CoverID != "" {
		coverBlobID := seedImmersiveBlob(t, server, transaction, coverPayload, "image/png", now)
		mustExecHTTPTest(t, transaction, `
INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES(?,?,?,'COVER',0,500,700,'image/png',?)
`, seed.CoverID, seed.GameID, coverBlobID, now)
	}
	if seed.VideoID != "" {
		videoBlobID := seedImmersiveBlob(t, server, transaction, videoPayload, "video/webm", now)
		mustExecHTTPTest(t, transaction, `
INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES(?,?,?,'VIDEO',0,NULL,NULL,'video/webm',?)
`, seed.VideoID, seed.GameID, videoBlobID, now)
	}
}

func seedImmersiveGame(t *testing.T, server *Server, seed immersiveGameSeed, now int64) {
	t.Helper()
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	mustExecHTTPTest(t, transaction, "PRAGMA defer_foreign_keys=ON")
	mustExecHTTPTest(t, transaction, `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,source_manifest_json,source_manifest_digest,
 status,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),?,?,?,'Retrom Studio','','Action',1,1999,
 'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','immersive-test','[]',?,'PUBLISHED',lower(?),1,?,?)
`, seed.GameID, seed.Title, gametitle.Initial(seed.Title), seed.Description,
		strings.Repeat(seed.GameID[len(seed.GameID)-1:], 64), seed.Title, now, now)
	seedImmersiveAssets(t, server, transaction, seed, "cover-"+seed.GameID, "video-"+seed.GameID, now)
	mustCommitHTTPTest(t, transaction)
}

func seedImmersivePlay(
	t *testing.T,
	server *Server,
	seed immersiveGameSeed,
	profileID string,
	startedAtMS, emulatorGameID int64,
) {
	t.Helper()
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	requireHTTPTestRuntimeTarget(t, transaction, "mgba")
	target, err := testsupport.LookupRuntimeTarget(t.Context(), transaction, "mgba")
	testassert.False(t, err != nil, err)
	variantID, launchID, playID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	mustExecHTTPTest(t, transaction, `
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,status,
 compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms
) VALUES(?,?,'mgba',?,?,NULL,?,'READY','READY','{}',NULL,1,0,0)
`, variantID, seed.GameID, target.ProviderID, target.TargetID, emulatorGameID)
	mustExecHTTPTest(t, transaction, `
INSERT INTO launch_sessions(
 id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,
 content_kind,dependency_snapshot_json,compatibility_code,
 return_to,credential_sha256,state,
 bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version
) VALUES(?,?,?,'mgba',?,?,?,'SINGLE_FILE','{}','READY','/',zeroblob(32),'FINISHED',?,?,?,?,?,1)
`, launchID, profileID, seed.GameID, target.ProviderID, target.TargetID, target.BundleSHA256,
		startedAtMS+1000, startedAtMS+500,
		startedAtMS+2000, startedAtMS, startedAtMS+500)
	mustExecHTTPTest(t, transaction, `
INSERT INTO play_sessions(
 id,launch_session_id,profile_id,game_id,started_at_ms,last_heartbeat_at_ms,
 ended_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,100,1,'FINISHED',1,?,?)
`, playID, launchID, profileID, seed.GameID, startedAtMS, startedAtMS+500,
		startedAtMS+500, startedAtMS, startedAtMS+500)
	mustCommitHTTPTest(t, transaction)
}

func TestImmersiveProjectionIsStableAndProfileIsolated(t *testing.T) {
	server := newTestServer(t)
	fixedNow := time.UnixMilli(10_000)
	server.now = func() time.Time { return fixedNow }
	seeds := []immersiveGameSeed{
		{GameID: "01980000-0000-7000-8000-00000000aa01", MetadataID: "01980000-0000-7000-8000-00000000ba01", ContentID: "01980000-0000-7000-8000-00000000ca01", Title: "alpha", Description: "Current alpha", CoverID: "01980000-0000-7000-8000-00000000da01", VideoID: "01980000-0000-7000-8000-00000000ea01"},
		{GameID: "01980000-0000-7000-8000-00000000aa02", MetadataID: "01980000-0000-7000-8000-00000000ba02", ContentID: "01980000-0000-7000-8000-00000000ca02", Title: "Alpha", Description: "Second alpha", CoverID: "01980000-0000-7000-8000-00000000da02"},
		{GameID: "01980000-0000-7000-8000-00000000aa03", MetadataID: "01980000-0000-7000-8000-00000000ba03", ContentID: "01980000-0000-7000-8000-00000000ca03", Title: "beta", Description: "Beta", CoverID: "01980000-0000-7000-8000-00000000da03"},
	}
	for index, seed := range seeds {
		seedImmersiveGame(t, server, seed, int64(1000+index))
	}
	seedImmersivePlay(t, server, seeds[0], "local", 4000, 101)
	seedImmersivePlay(t, server, seeds[1], "local", 5000, 102)
	otherProfile := "01980000-0000-7000-8000-00000000f002"
	mustExecHTTPTest(t, server.database, "INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Other',0)", otherProfile)
	seedImmersivePlay(t, server, seeds[2], otherProfile, 9000, 103)

	platforms := immersiveGET(t, server, "/api/v1/immersive/platforms")
	platformPage := decodeImmersiveResponse[immersivePlatformResponse](t, platforms)
	testassert.Falsef(t, platforms.Code != http.StatusOK, "platforms = %d %s", platforms.Code, platforms.Body.String())
	testassert.Falsef(t, platformPage.GeneratedAtMS != fixedNow.UnixMilli(),
		"platform page = %#v", platformPage)
	localGBA := findImmersivePlatform(platformPage.Items, "gba")
	testassert.Falsef(t, localGBA == nil || localGBA.GameCount != 3 || localGBA.LastPlayedAtMS == nil ||
		*localGBA.LastPlayedAtMS != 5000, "local GBA platform = %#v", localGBA)
	assertImmersiveFeaturedGameOrder(t, localGBA.FeaturedGames,
		seeds[1].GameID, seeds[0].GameID, seeds[2].GameID)
	testassert.Falsef(t, localGBA.FeaturedGames[0].CoverURL == nil,
		"local GBA featured cover = %#v", localGBA.FeaturedGames[0])
	testassert.Falsef(t, *localGBA.FeaturedGames[0].CoverURL != "/content/assets/"+seeds[1].CoverID ||
		localGBA.FeaturedGames[2].LastPlayedAtMS != nil,
		"local GBA featured games = %#v", localGBA.FeaturedGames)

	first := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games?limit=2")
	firstPage := decodeImmersiveResponse[immersiveGameResponse](t, first)
	testassert.Falsef(t, testassert.Any(
		func() bool { return first.Code != http.StatusOK },
		func() bool { return len(firstPage.Items) != 2 },
		func() bool { return firstPage.Items[0].GameID != seeds[0].GameID },
		func() bool { return firstPage.Items[1].GameID != seeds[1].GameID },
		func() bool { return firstPage.NextCursor == nil },
		func() bool { return firstPage.Items[0].CoverURL == nil || firstPage.Items[0].VideoURL == nil },
		func() bool {
			return firstPage.Items[0].PlatformInstance.ID == "" || firstPage.Items[0].DefaultCore.ID != "mgba"
		},
	), "first page = %d %#v", first.Code, firstPage)
	second := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games?limit=2&cursor="+
		url.QueryEscape(*firstPage.NextCursor))
	secondPage := decodeImmersiveResponse[immersiveGameResponse](t, second)
	testassert.Falsef(t, second.Code != http.StatusOK || len(secondPage.Items) != 1 ||
		secondPage.Items[0].GameID != seeds[2].GameID || secondPage.NextCursor != nil,
		"second page = %d %#v", second.Code, secondPage)
	changedLimit := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games?limit=1&cursor="+
		url.QueryEscape(*firstPage.NextCursor))
	testassert.Falsef(t, changedLimit.Code != http.StatusBadRequest ||
		!strings.Contains(changedLimit.Body.String(), `"code":"INVALID_CURSOR"`),
		"limit-bound cursor = %d %s", changedLimit.Code, changedLimit.Body.String())

	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: uuid.NewString(), ProfileID: otherProfile, Username: "other", DisplayName: "Other", Role: "USER",
	}}
	otherPlatforms := decodeImmersiveResponse[immersivePlatformResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/platforms"))
	otherGBA := findImmersivePlatform(otherPlatforms.Items, "gba")
	testassert.Falsef(t, otherGBA == nil || otherGBA.LastPlayedAtMS == nil || *otherGBA.LastPlayedAtMS != 9000,
		"other GBA platform = %#v", otherGBA)
	assertImmersiveFeaturedGameOrder(t, otherGBA.FeaturedGames,
		seeds[2].GameID, seeds[1].GameID, seeds[0].GameID)
	foreignCursor := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games?limit=2&cursor="+
		url.QueryEscape(*firstPage.NextCursor))
	testassert.Falsef(t, foreignCursor.Code != http.StatusBadRequest ||
		!strings.Contains(foreignCursor.Body.String(), `"code":"INVALID_CURSOR"`),
		"foreign cursor = %d %s", foreignCursor.Code, foreignCursor.Body.String())
}

func TestImmersiveQueriesFailClosedAndUnavailablePlatformsDoNotLeak(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{
		"/api/v1/immersive/platforms?unknown=true",
		"/api/v1/immersive/platforms/gba/games?unknown=true",
	} {
		response := immersiveGET(t, server, path)
		testassert.Falsef(t, response.Code != http.StatusBadRequest ||
			!strings.Contains(response.Body.String(), `"code":"INVALID_QUERY"`),
			"unknown query %s = %d %s", path, response.Code, response.Body.String())
	}
	missing := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games")
	testassert.Falsef(t, missing.Code != http.StatusNotFound ||
		!strings.Contains(missing.Body.String(), `"code":"RESOURCE_NOT_FOUND"`),
		"empty platform = %d %s", missing.Code, missing.Body.String())
	seed := immersiveGameSeed{
		GameID: "01980000-0000-7000-8000-00000000ab01", MetadataID: "01980000-0000-7000-8000-00000000bb01",
		ContentID: "01980000-0000-7000-8000-00000000cb01", Title: "Cursor", Description: "Cursor",
	}
	seedImmersiveGame(t, server, seed, 1000)
	invalidCursor := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games?cursor=not-signed")
	testassert.Falsef(t, invalidCursor.Code != http.StatusBadRequest ||
		!strings.Contains(invalidCursor.Body.String(), `"code":"INVALID_CURSOR"`),
		"invalid cursor = %d %s", invalidCursor.Code, invalidCursor.Body.String())
	impact, err := payloadrelease.GameDeleteImpact(context.Background(), server.database, seed.GameID)
	testassert.False(t, err != nil, err)
	deleteRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		"/api/v1/admin/games/"+seed.GameID,
		strings.NewReader(fmt.Sprintf(`{"confirmTitle":%q,"impactDigest":%q}`, seed.Title, impact.ImpactDigest)),
	)
	deleteRequest.Header.Set("Content-Type", "application/json")
	deleteRequest.Header.Set("If-Match", `"v1"`)
	deleteRequest.Header.Set("Idempotency-Key", uuid.NewString())
	cookie, csrf := testSessionCredentials()
	setCSRFCredentials(deleteRequest, cookie, csrf)
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	testassert.Falsef(t, deleted.Code != http.StatusAccepted, "hard delete = %d %s",
		deleted.Code, deleted.Body.String())
	deletedPlatform := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games")
	testassert.Falsef(t, deletedPlatform.Code != http.StatusNotFound, "deleted game platform = %d %s",
		deletedPlatform.Code, deletedPlatform.Body.String())
	seedImmersiveGame(t, server, immersiveGameSeed{
		GameID: "01980000-0000-7000-8000-00000000ab02", MetadataID: "01980000-0000-7000-8000-00000000bb02",
		ContentID: "01980000-0000-7000-8000-00000000cb02", Title: "Disabled", Description: "Disabled",
	}, 1900)
	mustExecHTTPTest(t, server.database, `
UPDATE platform_instances SET enabled=0,version=version+1,updated_at_ms=2000
WHERE catalog_template_key='gba/mgba'
`)
	platforms := decodeImmersiveResponse[immersivePlatformResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/platforms"))
	for _, platform := range platforms.Items {
		testassert.Falsef(t, platform.PlatformID == "gba", "disabled GBA leaked: %#v", platform)
	}
	disabled := immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games")
	testassert.Falsef(t, disabled.Code != http.StatusNotFound, "disabled platform = %d %s",
		disabled.Code, disabled.Body.String())
}

func replaceImmersiveMetadata(t *testing.T, server *Server, game immersiveGameSeed, replacement immersiveGameSeed) {
	t.Helper()
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	mustExecHTTPTest(t, transaction, "DELETE FROM game_assets WHERE game_id=?", game.GameID)
	seedImmersiveAssets(t, server, transaction, replacement, "replacement-cover", "replacement-video", 2000)
	mustExecHTTPTest(t, transaction, `
UPDATE games SET title=?,title_initial=?,description=?,developer='New Studio',publisher='',genre='Adventure',
 players=1,release_year=2001,metadata_source_kind='ADMIN_EDIT',metadata_source_ref_id=NULL,
 search_text=lower(?),version=version+1,updated_at_ms=2000 WHERE id=?
`, replacement.Title, gametitle.Initial(replacement.Title), replacement.Description, replacement.Title, game.GameID)
	mustCommitHTTPTest(t, transaction)
}

func TestImmersiveMediaAlwaysUsesCurrentAssetsAndRemovedAssetsDisappear(t *testing.T) {
	server := newTestServer(t)
	original := immersiveGameSeed{
		GameID: "01980000-0000-7000-8000-00000000ac01", MetadataID: "01980000-0000-7000-8000-00000000bc01",
		ContentID: "01980000-0000-7000-8000-00000000cc01", Title: "Original", Description: "Old text",
		CoverID: "01980000-0000-7000-8000-00000000dc01", VideoID: "01980000-0000-7000-8000-00000000ec01",
	}
	seedImmersiveGame(t, server, original, 1000)
	replacement := immersiveGameSeed{
		GameID: original.GameID, MetadataID: "01980000-0000-7000-8000-00000000bc02", ContentID: original.ContentID,
		Title: "Replacement", Description: "Current text", CoverID: "01980000-0000-7000-8000-00000000dc02",
		VideoID: "01980000-0000-7000-8000-00000000ec02",
	}
	replaceImmersiveMetadata(t, server, original, replacement)
	page := decodeImmersiveResponse[immersiveGameResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games"))
	testassert.Falsef(t, len(page.Items) != 1 || page.Items[0].Title != replacement.Title ||
		page.Items[0].Description != replacement.Description || page.Items[0].CoverURL == nil ||
		*page.Items[0].CoverURL != "/content/assets/"+replacement.CoverID || page.Items[0].VideoURL == nil ||
		*page.Items[0].VideoURL != "/content/assets/"+replacement.VideoID,
		"replacement projection = %#v", page)
	platformPage := decodeImmersiveResponse[immersivePlatformResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/platforms"))
	gba := findImmersivePlatform(platformPage.Items, "gba")
	testassert.Falsef(t, gba == nil || len(gba.FeaturedGames) != 1 ||
		gba.FeaturedGames[0].CoverURL == nil ||
		*gba.FeaturedGames[0].CoverURL != "/content/assets/"+replacement.CoverID,
		"replacement featured cover = %#v", gba)
	for assetID, payload := range map[string]string{
		replacement.CoverID: "replacement-cover",
		replacement.VideoID: "replacement-video",
	} {
		current := immersiveGET(t, server, "/content/assets/"+assetID)
		testassert.Falsef(t, current.Code != http.StatusOK || current.Body.String() != payload,
			"current asset %s = %d %q", assetID, current.Code, current.Body.String())
	}
	for _, assetID := range []string{original.CoverID, original.VideoID} {
		retired := immersiveGET(t, server, "/content/assets/"+assetID)
		testassert.Falsef(t, retired.Code != http.StatusNotFound, "retired asset %s = %d %s",
			assetID, retired.Code, retired.Body.String())
	}
	deleteVideoRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		"/api/v1/admin/games/"+original.GameID+"/assets/VIDEO",
		nil,
	)
	deleteVideoRequest.Header.Set("If-Match", `"v2"`)
	deleteVideoRequest.Header.Set("Idempotency-Key", uuid.NewString())
	deleteVideo := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteVideo, deleteVideoRequest)
	testassert.Falsef(t, deleteVideo.Code != http.StatusNoContent, "delete video = %d %s",
		deleteVideo.Code, deleteVideo.Body.String())
	afterRemoval := decodeImmersiveResponse[immersiveGameResponse](t,
		immersiveGET(t, server, "/api/v1/immersive/platforms/gba/games"))
	testassert.Falsef(t, len(afterRemoval.Items) != 1 || afterRemoval.Items[0].VideoURL != nil ||
		afterRemoval.Items[0].CoverURL == nil,
		"removed video projection = %#v", afterRemoval)
	removed := immersiveGET(t, server, "/content/assets/"+replacement.VideoID)
	testassert.Falsef(t, removed.Code != http.StatusNotFound, "removed video = %d %s",
		removed.Code, removed.Body.String())
}
