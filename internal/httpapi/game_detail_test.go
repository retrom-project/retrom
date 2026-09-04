package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func anyTrue(values ...bool) bool {
	for _, value := range values {
		if value {
			return true
		}
	}
	return false
}

type httpTestSQLExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func mustExecHTTPTest(t *testing.T, execer httpTestSQLExecer, query string, arguments ...any) {
	t.Helper()
	_, err := execer.ExecContext(context.Background(), query, arguments...)
	testassert.False(t, err != nil, err)
}

func requireHTTPTestRuntimeTarget(
	t *testing.T,
	execer httpTestSQLExecer,
	coreID string,
) {
	t.Helper()
	if _, err := testsupport.LookupRuntimeTarget(context.Background(), execer, coreID); err != nil {
		t.Fatal(err)
	}
}

func mustDecodeHTTPTest(t *testing.T, contents []byte, destination any) {
	t.Helper()
	testassert.False(t, json.Unmarshal(contents, destination) != nil, "decode HTTP test response")
}

func mustCommitHTTPTest(t *testing.T, transaction *sql.Tx) {
	t.Helper()
	testassert.False(t, transaction.Commit() != nil, "commit HTTP test fixture")
}

type httpTestScanner interface {
	Scan(...any) error
}

func mustScanHTTPTest(t *testing.T, scanner httpTestScanner, destinations ...any) {
	t.Helper()
	testassert.False(t, scanner.Scan(destinations...) != nil, "scan HTTP test fixture")
}

func TestGameDetailReturnsCoreValidationChoicesAndDOSPrograms(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	gameID := "01980000-0000-7000-8000-000000000101"
	metadataID := "01980000-0000-7000-8000-000000000102"
	contentID := "01980000-0000-7000-8000-000000000103"
	coverBlobID := "01980000-0000-7000-8000-000000000104"
	coverAssetID := "01980000-0000-7000-8000-000000000105"
	videoAssetID := "01980000-0000-7000-8000-000000000110"
	variantID := "01980000-0000-7000-8000-000000000106"
	variantRevisionID := "01980000-0000-7000-8000-000000000107"
	saveStateID := "01980000-0000-7000-8000-000000000108"
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	now := time.Now().UnixMilli()
	fixture := gameDetailSeed{now: now}
	seedGameDetailMedia(t, server, transaction, gameID, metadataID, contentID, coverBlobID, coverAssetID, videoAssetID, &fixture)
	seedGameDetailRuntime(t, server, transaction, gameID, contentID, variantID, variantRevisionID, saveStateID, &fixture)
	videoPayload, screenshot := fixture.videoPayload, fixture.screenshot
	videoMetadata := fixture.videoMetadata
	latestLaunchID, videoBlobID, screenshotBlobID := fixture.latestLaunchID, fixture.videoBlobID, fixture.screenshotBlobID
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games/"+gameID, nil))
	testassert.Falsef(t, recorder.Code != http.StatusOK, "game detail status = %d: %s", recorder.Code, recorder.Body.String())
	var response struct {
		DefaultDOSEntry *string `json:"defaultDosEntry"`
		CoverURL        *string `json:"coverUrl"`
		VideoURL        *string `json:"videoUrl"`
		CoreOptions     []struct {
			CoreID string `json:"coreId"`
			Status string `json:"status"`
		} `json:"coreOptions"`
		DOSEntries []struct {
			Path             string `json:"path"`
			DirectLaunchSafe bool   `json:"directLaunchSafe"`
		} `json:"dosEntries"`
		SaveStates []struct {
			SaveStateID   string `json:"saveStateId"`
			ScreenshotURL string `json:"screenshotUrl"`
		} `json:"saveStates"`
		SaveStateCount int64 `json:"saveStateCount"`
	}
	mustDecodeHTTPTest(t, recorder.Body.Bytes(), &response)
	testassert.Falsef(t, testassert.Any(func() bool { return len(response.CoreOptions) != 1 }, func() bool { return response.CoreOptions[0].CoreID != "dosbox_pure" }, func() bool { return response.CoreOptions[0].Status != "READY" }), "core options = %#v", response.CoreOptions)
	expectedCoverURL := "/content/assets/" + coverAssetID
	expectedVideoURL := "/content/assets/" + videoAssetID
	testassert.Falsef(t, testassert.Any(func() bool { return response.CoverURL == nil }, func() bool { return *response.CoverURL != expectedCoverURL }, func() bool { return response.DefaultDOSEntry != nil }, func() bool { return response.VideoURL == nil }, func() bool { return *response.VideoURL != expectedVideoURL }, func() bool { return len(response.DOSEntries) != 2 }, func() bool { return response.DOSEntries[0].Path != "GAMES/DOOM.EXE" }, func() bool { return !response.DOSEntries[0].DirectLaunchSafe }, func() bool { return response.DOSEntries[1].DirectLaunchSafe }, func() bool { return len(response.SaveStates) != 8 }, func() bool { return response.SaveStateCount != 9 }), "DOS choices = default:%v entries:%#v", response.DefaultDOSEntry, response.DOSEntries)
	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games?limit=100", nil))
	testassert.Falsef(t, anyTrue(
		list.Code != http.StatusOK,
		!strings.Contains(list.Body.String(), `"coverUrl":"`+expectedCoverURL+`"`),
		strings.Contains(list.Body.String(), `"videoUrl"`),
		!strings.Contains(list.Body.String(), `"defaultCore":{"id":"dosbox_pure","name":"DOSBox Pure"}`),
		!strings.Contains(list.Body.String(), `"lastPlayedAtMs":`),
		!strings.Contains(list.Body.String(), `"createdAtMs":`),
		!strings.Contains(list.Body.String(), `"generatedAtMs":`),
	), "game list cover = %d: %s", list.Code, list.Body.String())
	videoRangeRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, expectedVideoURL, nil)
	videoRangeRequest.Header.Set("Range", "bytes=4-7")
	videoRange := httptest.NewRecorder()
	server.Handler().ServeHTTP(videoRange, videoRangeRequest)
	testassert.Falsef(t, anyTrue(videoRange.Code != http.StatusPartialContent, videoRange.Body.String() != "ftyp",
		videoRange.Header().Get("Content-Type") != "video/mp4", videoRange.Header().Get("Accept-Ranges") != "bytes"),
		"video range = %d headers=%v body=%q", videoRange.Code, videoRange.Header(), videoRange.Body.String())
	videoHead := httptest.NewRecorder()
	server.Handler().ServeHTTP(videoHead, httptest.NewRequestWithContext(context.Background(), http.MethodHead, expectedVideoURL, nil))
	testassert.Falsef(t, anyTrue(videoHead.Code != http.StatusOK, videoHead.Body.Len() != 0,
		videoHead.Header().Get("Content-Length") != strconv.Itoa(len(videoPayload)),
		videoHead.Header().Get("Content-Type") != "video/mp4"),
		"video HEAD = %d headers=%v body=%q", videoHead.Code, videoHead.Header(), videoHead.Body.String())
	adminList := httptest.NewRecorder()
	server.Handler().ServeHTTP(adminList, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/games?limit=100", nil))
	testassert.Falsef(t, anyTrue(adminList.Code != http.StatusOK,
		!strings.Contains(adminList.Body.String(), `"releaseYear":`),
		!strings.Contains(adminList.Body.String(), `"metadataComplete":`),
		!strings.Contains(adminList.Body.String(), `"runtimeStatus":"READY"`)),
		"admin game health projection = %d: %s", adminList.Code, adminList.Body.String())
	adminDetail := httptest.NewRecorder()
	server.Handler().ServeHTTP(adminDetail, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/games/"+gameID, nil))
	testassert.Falsef(t, testassert.Any(func() bool { return adminDetail.Code != http.StatusOK }, func() bool { return !strings.Contains(adminDetail.Body.String(), `"generatedAtMs":`) }), "admin game detail generated time = %d: %s", adminDetail.Code, adminDetail.Body.String())
	saves := httptest.NewRecorder()
	server.Handler().ServeHTTP(saves, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/saves", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return saves.Code != http.StatusOK }, func() bool {
		return !strings.Contains(saves.Body.String(), `"screenshotUrl":"`+saveStateScreenshotURL(saveStateID)+`"`)
	}, func() bool {
		return !strings.Contains(saves.Body.String(), `"sizeBytes":`+strconv.Itoa(len(screenshot)))
	}, func() bool { return !strings.Contains(saves.Body.String(), `"activeDurationMs":180000`) }, func() bool { return !strings.Contains(saves.Body.String(), `"platform":{"id":"dos","name":"MS-DOS"}`) }, func() bool { return !strings.Contains(saves.Body.String(), `"generatedAtMs":`) }), "save list projection = %d: %s", saves.Code, saves.Body.String())
	filteredSaves := httptest.NewRecorder()
	server.Handler().ServeHTTP(filteredSaves, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/saves?gameId="+gameID, nil))
	testassert.Falsef(t, testassert.Any(func() bool { return filteredSaves.Code != http.StatusOK }, func() bool { return !strings.Contains(filteredSaves.Body.String(), `"gameId":"`+gameID+`"`) }), "save game filter = %d: %s", filteredSaves.Code, filteredSaves.Body.String())
	missingGameSaves := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingGameSaves, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/saves?gameId="+uuid.NewString(), nil))
	testassert.Falsef(t, testassert.Any(func() bool { return missingGameSaves.Code != http.StatusOK }, func() bool { return !strings.Contains(missingGameSaves.Body.String(), `"items":[]`) }), "save missing game filter = %d: %s", missingGameSaves.Code, missingGameSaves.Body.String())
	assertGameHomeAndActivity(t, server, gameID, screenshotBlobID, latestLaunchID, now, expectedCoverURL, saveStateID, screenshot)
	assertScreenshotlessSaveProjections(t, server, gameID)
	assertGameProfileIsolation(t, server, gameID, saveStateID, now)
	assertGameAdminMutations(t, server, gameID, contentID, coverBlobID, now, videoPayload, videoMetadata, videoBlobID)
}

func assertScreenshotlessSaveProjections(t *testing.T, server *Server, gameID string) {
	t.Helper()
	mustExecHTTPTest(t, server.database, "UPDATE save_states SET screenshot_blob_id=NULL WHERE game_id=?", gameID)
	for _, path := range []string{
		"/api/v1/saves?gameId=" + gameID,
		"/api/v1/games/" + gameID,
		"/api/v1/home",
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		testassert.Falsef(t, testassert.Any(
			func() bool { return response.Code != http.StatusOK },
			func() bool { return !strings.Contains(response.Body.String(), `"screenshotUrl":null`) },
			func() bool { return strings.Contains(response.Body.String(), "/content/save-states/") },
		), "screenshot-less projection %s = %d: %s", path, response.Code, response.Body.String())
	}
}

func assertGameHomeAndActivity(
	t *testing.T, server *Server,
	gameID, screenshotBlobID, latestLaunchID string,
	now int64, expectedCoverURL, saveStateID string, screenshot []byte,
) {
	home := httptest.NewRecorder()
	server.Handler().ServeHTTP(home, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return home.Code != http.StatusOK }, func() bool { return !strings.Contains(home.Body.String(), `"recentSaves":[{"activeDurationMs":60000`) }), "home recent save duration = %d: %s", home.Code, home.Body.String())
	var homeResponse struct {
		FeaturedGame *struct {
			GameID          string `json:"gameId"`
			HasSaveStates   bool   `json:"hasSaveStates"`
			LastSessionSave *struct {
				SaveStateID string `json:"saveStateId"`
			} `json:"lastSessionSave"`
		} `json:"featuredGame"`
		RecentGames []struct {
			GameID       string `json:"gameId"`
			SessionCount int64  `json:"sessionCount"`
		} `json:"recentGames"`
		LatestGames []struct {
			GameID      string `json:"gameId"`
			Title       string `json:"title"`
			CreatedAtMS int64  `json:"createdAtMs"`
		} `json:"latestGames"`
		QuickPlatforms []homePlatform `json:"quickPlatforms"`
	}
	mustDecodeHTTPTest(t, home.Body.Bytes(), &homeResponse)
	testassert.Falsef(t, testassert.Any(func() bool { return homeResponse.FeaturedGame == nil }, func() bool { return homeResponse.FeaturedGame.GameID != gameID }, func() bool { return !homeResponse.FeaturedGame.HasSaveStates }, func() bool { return homeResponse.FeaturedGame.LastSessionSave != nil }, func() bool { return len(homeResponse.RecentGames) != 1 }, func() bool { return homeResponse.RecentGames[0].SessionCount != 2 }, func() bool { return len(homeResponse.LatestGames) != 1 }, func() bool { return homeResponse.LatestGames[0].GameID != gameID }, func() bool { return homeResponse.LatestGames[0].CreatedAtMS != now }, func() bool { return len(homeResponse.QuickPlatforms) != 4 }, func() bool { return homeResponse.QuickPlatforms[0].ID != "dos" }, func() bool { return homeResponse.QuickPlatforms[0].PlayCount != 2 }), "home projection = %#v", homeResponse)
	sessionSaveID := uuid.NewString()
	payloadDigest := sha256.Sum256(screenshot)
	mustExecHTTPTest(t, server.database, `
INSERT INTO save_states(
 id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,payload_size_bytes,
 screenshot_blob_id,source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms
) VALUES(?,'local',?,'test-checkpoint-v1',?,?,?,?,?,'本次游玩存档',240000,1,?,?,NULL)
`, sessionSaveID, gameID, screenshotBlobID, hex.EncodeToString(payloadDigest[:]), len(screenshot),
		screenshotBlobID, latestLaunchID, now+20, now+20)
	var alternateLaunchID string
	if err := server.database.QueryRowContext(
		context.Background(),
		`SELECT id FROM launch_sessions WHERE game_id=? AND id<>? ORDER BY id LIMIT 1`,
		gameID,
		latestLaunchID,
	).Scan(&alternateLaunchID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(
		context.Background(), `UPDATE save_states SET source_launch_session_id=? WHERE id=?`, alternateLaunchID, sessionSaveID,
	); err == nil ||
		!strings.Contains(err.Error(), "immutable") {
		t.Fatalf("mutable save source error = %v", err)
	}
	homeWithSessionSave := httptest.NewRecorder()
	server.Handler().ServeHTTP(homeWithSessionSave, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil))
	testassert.Falsef(t, homeWithSessionSave.Code != http.StatusOK, "home with session save = %d: %s", homeWithSessionSave.Code, homeWithSessionSave.Body.String())
	mustDecodeHTTPTest(t, homeWithSessionSave.Body.Bytes(), &homeResponse)
	testassert.Falsef(t, testassert.Any(func() bool { return homeResponse.FeaturedGame == nil }, func() bool { return homeResponse.FeaturedGame.LastSessionSave == nil }, func() bool { return homeResponse.FeaturedGame.LastSessionSave.SaveStateID != sessionSaveID }), "featured session save = %#v", homeResponse.FeaturedGame)
	seedRecentGameHistory(t, server.database, now, 55)
	latest := httptest.NewRecorder()
	server.Handler().ServeHTTP(latest, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil))
	testassert.Falsef(t, latest.Code != http.StatusOK, "home latest games = %d: %s", latest.Code, latest.Body.String())
	var latestResponse struct {
		LatestGames []struct {
			Title       string `json:"title"`
			CreatedAtMS int64  `json:"createdAtMs"`
		} `json:"latestGames"`
	}
	mustDecodeHTTPTest(t, latest.Body.Bytes(), &latestResponse)
	testassert.Falsef(t, testassert.Any(func() bool { return len(latestResponse.LatestGames) != 10 }, func() bool { return latestResponse.LatestGames[0].Title != "Recent fixture 54" }, func() bool { return latestResponse.LatestGames[9].Title != "Recent fixture 45" }), "latest game order = %#v", latestResponse.LatestGames)
	for index := 1; index < len(latestResponse.LatestGames); index++ {
		testassert.Falsef(t, latestResponse.LatestGames[index-1].CreatedAtMS <= latestResponse.LatestGames[index].CreatedAtMS, "latest game timestamps are not descending: %#v", latestResponse.LatestGames)
	}
	recent := httptest.NewRecorder()
	server.Handler().ServeHTTP(recent, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/recent-games", nil))
	testassert.Falsef(t, anyTrue(recent.Code != http.StatusOK,
		!strings.Contains(recent.Body.String(), `"activeDurationMs":360000`),
		!strings.Contains(recent.Body.String(), `"sessionCount":2`), strings.Contains(recent.Body.String(), `"limit"`),
		!strings.Contains(recent.Body.String(), `"coverUrl":"`+expectedCoverURL+`"`),
		!strings.Contains(recent.Body.String(), `"generatedAtMs":`)),
		"recent games projection = %d: %s", recent.Code, recent.Body.String())
	var recentResponse struct {
		Items []recentGameProjection `json:"items"`
	}
	mustDecodeHTTPTest(t, recent.Body.Bytes(), &recentResponse)
	testassert.Falsef(t, len(recentResponse.Items) != 56,
		"unbounded recent games count = %d", len(recentResponse.Items))
	invalidRecentLimit := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidRecentLimit, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/recent-games?limit=50", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return invalidRecentLimit.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(invalidRecentLimit.Body.String(), `"code":"INVALID_QUERY"`) }), "recent games invalid limit = %d: %s", invalidRecentLimit.Code, invalidRecentLimit.Body.String())
	screenshotResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		screenshotResponse,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, saveStateScreenshotURL(saveStateID), nil),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return screenshotResponse.Code != http.StatusOK }, func() bool { return screenshotResponse.Body.String() != string(screenshot) }, func() bool { return screenshotResponse.Header().Get("Cache-Control") != "private, no-store" }, func() bool { return screenshotResponse.Header().Get("Content-Type") != "image/png" }), "save screenshot = %d headers=%v body=%q", screenshotResponse.Code, screenshotResponse.Header(), screenshotResponse.Body.String())
}

func assertGameProfileIsolation(t *testing.T, server *Server, gameID, saveStateID string, now int64) {
	localPage := httptest.NewRecorder()
	server.Handler().ServeHTTP(localPage, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/saves?limit=1", nil))
	var localPageBody struct {
		NextCursor *string `json:"nextCursor"`
	}
	mustDecodeHTTPTest(t, localPage.Body.Bytes(), &localPageBody)
	testassert.Falsef(t, localPageBody.NextCursor == nil,
		"local save cursor = %d %s", localPage.Code, localPage.Body.String())
	const otherProfileID = "01980000-0000-7000-8000-000000009997"
	mustExecHTTPTest(t, server.database,
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Other Player',?)`, otherProfileID, now)
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: "01980000-0000-7000-8000-000000009996", ProfileID: otherProfileID, Username: "other-user",
		DisplayName: "Other Player", Role: "ADMIN", SessionID: "01980000-0000-7000-8000-000000009995",
	}}
	otherDetail := httptest.NewRecorder()
	server.Handler().ServeHTTP(otherDetail, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games/"+gameID, nil))
	testassert.Falsef(t, testassert.Any(func() bool { return otherDetail.Code != http.StatusOK }, func() bool { return !strings.Contains(otherDetail.Body.String(), `"activeDurationMs":0`) }, func() bool { return !strings.Contains(otherDetail.Body.String(), `"saveStateCount":0`) }, func() bool { return !strings.Contains(otherDetail.Body.String(), `"saveStates":[]`) }), "other profile game detail = %d: %s", otherDetail.Code, otherDetail.Body.String())
	for path, expectedFragment := range map[string]string{
		"/api/v1/saves":        `"items":[]`,
		"/api/v1/recent-games": `"items":[]`,
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		testassert.Falsef(t, testassert.Any(func() bool { return response.Code != http.StatusOK }, func() bool { return !strings.Contains(response.Body.String(), expectedFragment) }), "other profile %s = %d: %s", path, response.Code, response.Body.String())
	}
	otherHome := httptest.NewRecorder()
	server.Handler().ServeHTTP(otherHome, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return otherHome.Code != http.StatusOK }, func() bool { return !strings.Contains(otherHome.Body.String(), `"recentSaves":[]`) }, func() bool { return !strings.Contains(otherHome.Body.String(), `"recentGames":[]`) }, func() bool { return !strings.Contains(otherHome.Body.String(), `"featuredGame":null`) }, func() bool { return !strings.Contains(otherHome.Body.String(), `"latestGames":[`) }), "other profile home = %d: %s", otherHome.Code, otherHome.Body.String())
	foreignCursor := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		foreignCursor,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/saves?limit=1&cursor="+url.QueryEscape(*localPageBody.NextCursor), nil),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return foreignCursor.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(foreignCursor.Body.String(), `"code":"INVALID_CURSOR"`) }), "cross-profile cursor = %d: %s", foreignCursor.Code, foreignCursor.Body.String())
	foreignScreenshot := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		foreignScreenshot,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, saveStateScreenshotURL(saveStateID), nil),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return foreignScreenshot.Code != http.StatusNotFound }, func() bool {
		return !strings.Contains(foreignScreenshot.Body.String(), `"code":"SAVE_SCREENSHOT_NOT_FOUND"`)
	}), "foreign screenshot = %d: %s", foreignScreenshot.Code, foreignScreenshot.Body.String())
	foreignPatchRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodPatch,
		"/api/v1/saves/"+saveStateID,
		strings.NewReader(`{"name":"Cross-account overwrite"}`),
	)
	foreignPatchRequest.Header.Set("Content-Type", "application/json")
	foreignPatchRequest.Header.Set("If-Match", `"v1"`)
	foreignPatch := httptest.NewRecorder()
	server.Handler().ServeHTTP(foreignPatch, foreignPatchRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return foreignPatch.Code != http.StatusNotFound }, func() bool { return !strings.Contains(foreignPatch.Body.String(), `"code":"SAVE_STATE_NOT_FOUND"`) }), "foreign save patch = %d: %s", foreignPatch.Code, foreignPatch.Body.String())
	foreignDeleteRequest := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/saves/"+saveStateID, nil)
	foreignDeleteRequest.Header.Set("If-Match", `"v1"`)
	foreignDelete := httptest.NewRecorder()
	server.Handler().ServeHTTP(foreignDelete, foreignDeleteRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return foreignDelete.Code != http.StatusNotFound }, func() bool { return !strings.Contains(foreignDelete.Body.String(), `"code":"SAVE_STATE_NOT_FOUND"`) }), "foreign save delete = %d: %s", foreignDelete.Code, foreignDelete.Body.String())
	var preservedName string
	var preservedDeletedAt sql.NullInt64
	if err := server.database.QueryRowContext(context.Background(),
		`SELECT name,deleted_at_ms FROM save_states WHERE id=?`,
		saveStateID,
	).Scan(&preservedName, &preservedDeletedAt); err != nil || preservedName != "入口存档" || preservedDeletedAt.Valid {
		t.Fatalf("foreign mutation changed save: name=%q deleted=%v error=%v", preservedName, preservedDeletedAt, err)
	}
	server.authenticator = testAuthenticator{}
	mustExecHTTPTest(t, server.database, "UPDATE save_states SET deleted_at_ms=? WHERE id=?", now+1, saveStateID)
	deletedScreenshot := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		deletedScreenshot,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, saveStateScreenshotURL(saveStateID), nil),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return deletedScreenshot.Code != http.StatusNotFound }, func() bool {
		return !strings.Contains(deletedScreenshot.Body.String(), `"code":"SAVE_SCREENSHOT_NOT_FOUND"`)
	}), "deleted save screenshot = %d: %s", deletedScreenshot.Code, deletedScreenshot.Body.String())
}

func assertGameAdminMutations(
	t *testing.T, server *Server, gameID, contentID, coverBlobID string, now int64,
	videoPayload []byte, videoMetadata blobstore.Metadata, videoBlobID string,
) {
	var originalCoverAssetID, originalVideoAssetID string
	mustScanHTTPTest(t, server.database.QueryRowContext(context.Background(), `
SELECT
 (SELECT id FROM game_assets WHERE game_id=? AND kind='COVER' ORDER BY created_at_ms,id LIMIT 1),
 (SELECT id FROM game_assets WHERE game_id=? AND kind='VIDEO' ORDER BY created_at_ms,id LIMIT 1)
`, gameID, gameID), &originalCoverAssetID, &originalVideoAssetID)
	candidateID, candidateAssetID := seedCompletedGameScrape(t, server.database, gameID, contentID, coverBlobID, now)
	candidates := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		candidates,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/games/"+gameID+"/scrape-candidates", nil),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return candidates.Code != http.StatusOK }, func() bool { return !strings.Contains(candidates.Body.String(), `"candidateId":"`+candidateID+`"`) }, func() bool {
		return !strings.Contains(candidates.Body.String(), `"candidateAssetId":"`+candidateAssetID+`"`)
	}, func() bool { return !strings.Contains(candidates.Body.String(), `"kind":"COVER"`) }), "game scrape candidates = %d: %s", candidates.Code, candidates.Body.String())
	apply := httptest.NewRecorder()
	applyRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost,
		"/api/v1/admin/games/"+gameID+"/scrape-candidates/"+candidateID+"/apply",
		strings.NewReader(
			`{"fields":["title"],"selectedAssets":{"coverCandidateAssetId":null,`+
				`"backgroundCandidateAssetId":null,"screenshotCandidateAssetIds":[]}}`,
		),
	)
	applyRequest.Header.Set("Content-Type", "application/json")
	applyRequest.Header.Set("If-Match", `"v1"`)
	applyRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.Handler().ServeHTTP(apply, applyRequest)
	testassert.Falsef(t, apply.Code != http.StatusOK, "apply game scrape candidate = %d: %s", apply.Code, apply.Body.String())
	var appliedTitle, appliedTitleInitial string
	var preservedAssets int64
	mustScanHTTPTest(t, server.database.QueryRowContext(context.Background(), `
SELECT m.title,m.title_initial,count(a.id)
FROM games g
JOIN games m ON m.id=g.id
LEFT JOIN game_assets a ON a.game_id=g.id AND a.game_id=m.id
WHERE g.id=?
GROUP BY m.title,m.title_initial
`, gameID), &appliedTitle, &appliedTitleInitial, &preservedAssets)
	testassert.Falsef(t, testassert.Any(
		func() bool { return appliedTitle != "Doom refreshed" },
		func() bool { return appliedTitleInitial != "D" },
		func() bool { return preservedAssets != 2 },
	), "applied title/initial/assets = %q/%s/%d", appliedTitle, appliedTitleInitial, preservedAssets)
	var preservedCoverID, preservedVideoID string
	mustScanHTTPTest(t, server.database.QueryRowContext(context.Background(), `
SELECT
 (SELECT id FROM game_assets WHERE game_id=? AND kind='COVER'),
 (SELECT id FROM game_assets WHERE game_id=? AND kind='VIDEO')
`, gameID, gameID), &preservedCoverID, &preservedVideoID)
	testassert.Falsef(t, preservedCoverID != originalCoverAssetID || preservedVideoID != originalVideoAssetID,
		"unselected candidate media changed: cover=%s video=%s", preservedCoverID, preservedVideoID)
	videoUploadID := "01980000-0000-7000-8000-000000000111"
	videoUploadFileID := "01980000-0000-7000-8000-000000000112"
	mustExecHTTPTest(t, server.database, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,?,?,1,?,?,?);
`, videoUploadID, len(videoPayload), videoMetadata.SHA256, now+60_000, now, now)
	mustExecHTTPTest(t, server.database, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,'preview.mp4',?,?,?,'COMPLETE',?,?)
`, videoUploadFileID, videoUploadID, len(videoPayload), len(videoPayload), videoBlobID, now, now)
	replaceVideo := httptest.NewRecorder()
	replaceVideoRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/games/"+gameID+"/assets", strings.NewReader(`{"uploadFileId":"`+videoUploadFileID+`","kind":"VIDEO","ordinal":0}`))
	replaceVideoRequest.Header.Set("Content-Type", "application/json")
	replaceVideoRequest.Header.Set("If-Match", `"v2"`)
	replaceVideoRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.Handler().ServeHTTP(replaceVideo, replaceVideoRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return replaceVideo.Code != http.StatusCreated }, func() bool { return !strings.Contains(replaceVideo.Body.String(), `"mediaType":"video/mp4"`) }, func() bool { return !strings.Contains(replaceVideo.Body.String(), `"widthPx":null`) }), "replace video = %d: %s", replaceVideo.Code, replaceVideo.Body.String())
	removeVideo := httptest.NewRecorder()
	removeVideoRequest := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/admin/games/"+gameID+"/assets/VIDEO", nil)
	removeVideoRequest.Header.Set("If-Match", `"v3"`)
	removeVideoRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.Handler().ServeHTTP(removeVideo, removeVideoRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return removeVideo.Code != http.StatusNoContent }, func() bool { return removeVideo.Header().Get("ETag") != `"v4"` }), "remove video = %d headers=%v: %s", removeVideo.Code, removeVideo.Header(), removeVideo.Body.String())
	var currentVideos, retiredAssets int
	var currentTitleInitial string
	if err := server.database.QueryRowContext(context.Background(), `
SELECT
(SELECT count(*) FROM game_assets asset JOIN games game ON game.id=asset.game_id WHERE game.id=? AND asset.kind='VIDEO'),
(SELECT count(*) FROM game_assets asset JOIN games game ON game.id=asset.game_id
 WHERE game.id=? AND asset.game_id<>game.id),
(SELECT metadata.title_initial FROM games game
 JOIN games metadata ON metadata.id=game.id WHERE game.id=?)
`, gameID, gameID, gameID).Scan(&currentVideos, &retiredAssets, &currentTitleInitial); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return currentVideos != 0 },
		func() bool { return retiredAssets != 0 },
		func() bool { return currentTitleInitial != "D" },
	), "retired game media = current videos:%d retired assets:%d title initial:%s",
		currentVideos, retiredAssets, currentTitleInitial)
}

func TestGameListUsesFilteredCursorPagesAndReturnsFacetsOnlyOnFirstPage(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	mustExecHTTPTest(t, transaction, `PRAGMA defer_foreign_keys=ON`)
	const baseTime = int64(1_786_000_000_000)
	gameIDs := []string{
		"01980000-0000-7000-8000-000000001001",
		"01980000-0000-7000-8000-000000001002",
		"01980000-0000-7000-8000-000000001003",
	}
	for index, gameID := range gameIDs {
		title := fmt.Sprintf("DOS Game %d", index+1)
		createdAt := baseTime + int64(index)*1000
		mustExecHTTPTest(t, transaction, `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='dos/dosbox_pure'),?,'D','','','','',NULL,NULL,
 'IMPORT_REVIEW','pagination-fixture','SINGLE_FILE','IMPORT_REVIEW','pagination-fixture','{}',?,
 'PUBLISHED',?,1,?,?)
`, gameID, title, strings.Repeat(strconv.Itoa(index+1), 64), strings.ToLower(title), createdAt, createdAt)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	type pageResponse struct {
		Items []struct {
			GameID string `json:"gameId"`
		} `json:"items"`
		NextCursor    *string `json:"nextCursor"`
		FilteredCount int64   `json:"filteredCount"`
		Facets        struct {
			TotalCount int64 `json:"totalCount"`
		} `json:"facets"`
	}
	first := httptest.NewRecorder()
	server.Handler().ServeHTTP(first, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/v1/games?sort=ADDED_DESC&platformId=dos&limit=2", nil,
	))
	testassert.Falsef(t, first.Code != http.StatusOK, "first game page = %d %s", first.Code, first.Body.String())
	var firstPage pageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(firstPage.Items) != 2 }, func() bool { return firstPage.Items[0].GameID != gameIDs[2] }, func() bool { return firstPage.Items[1].GameID != gameIDs[1] }, func() bool { return firstPage.NextCursor == nil }, func() bool { return firstPage.FilteredCount != 3 }, func() bool { return firstPage.Facets.TotalCount != 3 }), "first page = %#v", firstPage)

	second := httptest.NewRecorder()
	server.Handler().ServeHTTP(second, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet,
		"/api/v1/games?sort=ADDED_DESC&platformId=dos&limit=2&cursor="+url.QueryEscape(*firstPage.NextCursor),
		nil,
	))
	testassert.Falsef(t, second.Code != http.StatusOK, "second game page = %d %s", second.Code, second.Body.String())
	var secondPage pageResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(secondPage.Items) != 1 }, func() bool { return secondPage.Items[0].GameID != gameIDs[0] }, func() bool { return secondPage.NextCursor != nil }, func() bool { return strings.Contains(second.Body.String(), `"facets"`) }, func() bool { return strings.Contains(second.Body.String(), `"filteredCount"`) }), "second page = %#v body=%s", secondPage, second.Body.String())
}

func seedCompletedGameScrape(
	t *testing.T,
	database *sql.DB,
	gameID, _ string, coverBlobID string,
	now int64,
) (string, string) {
	t.Helper()
	jobID, runID := uuid.NewString(), uuid.NewString()
	responseID, candidateID, candidateAssetID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,
max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'GAME',?,'METADATA_SCRAPE',?,1,'{}',0,'SUCCEEDED',1,2,?,?,?,?)
`, jobID, gameID, strings.Repeat("7", 64), now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO metadata_provider_responses(id,provider,request_digest,http_status,outcome,raw_response_blob_id,
raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES(?,'HASHEOUS',?,200,'HIT',NULL,'NONE',?,?)
`, responseID, strings.Repeat("8", 64), now, now+60_000); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO metadata_scrape_runs(id,import_item_id,game_id,job_id,provider,
provider_config_version,state,version,created_at_ms,updated_at_ms,completed_at_ms,error_code)
VALUES(?,NULL,?,?,'HASHEOUS',1,'COMPLETED',1,?,?,?,NULL)
`, runID, gameID, jobID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO scrape_candidates(id,scrape_run_id,primary_response_id,provider_game_id,normalized_metadata_json,
evidence_json,created_at_ms)
VALUES(?,?,?,'doom-refreshed','{"title":"Doom refreshed","description":"Updated"}','{}',?)
`, candidateID, runID, responseID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO scrape_candidate_assets(id,scrape_candidate_id,provider_response_id,provider_asset_id,kind_hint,
ordinal,source_path,status,blob_id,width_px,height_px,media_type,error_code,fetched_at_ms,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'cover','COVER',0,'/cover','READY',?,600,800,'image/png',NULL,?,1,?,?)
`, candidateAssetID, candidateID, responseID, coverBlobID, now, now, now); err != nil {
		t.Fatal(err)
	}
	return candidateID, candidateAssetID
}

func seedRecentGameHistory(t *testing.T, database *sql.DB, now int64, count int) {
	t.Helper()
	target, err := testsupport.LookupRuntimeTarget(t.Context(), database, "dosbox_pure")
	testassert.False(t, err != nil, err)
	transaction, err := database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	mustExecHTTPTest(t, transaction, "PRAGMA defer_foreign_keys=ON")
	for index := 0; index < count; index++ {
		gameID := uuid.NewString()
		variantID := uuid.NewString()
		launchID := uuid.NewString()
		playID := uuid.NewString()
		mustExecHTTPTest(t, transaction, `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,source_manifest_json,source_manifest_digest,
 status,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='dos/dosbox_pure'),?,'R','','','','',NULL,NULL,
 'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE',?,'{}',?,'PUBLISHED',?,1,?,?)
`, gameID, fmt.Sprintf("Recent fixture %02d", index), fmt.Sprintf("recent-%d", index), strings.Repeat("7", 64),
			fmt.Sprintf("recent fixture %02d", index), now+int64(index), now+int64(index))
		mustExecHTTPTest(t, transaction, `
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,emulator_game_id,status,compatibility_code,
 dependency_snapshot_json,version,created_at_ms,updated_at_ms
) VALUES(?,?,'dosbox_pure',?,?,?,'READY','READY','{}',1,?,?)
`, variantID, gameID, target.ProviderID, target.TargetID, 10_000+index, now, now)
		mustExecHTTPTest(t, transaction, `
INSERT INTO launch_sessions(id,profile_id,purpose,game_id,core_id,provider_id,target_id,bundle_sha256,
content_kind,dependency_snapshot_json,compatibility_code,return_to,credential_sha256,
state,bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version)
VALUES(?,'local','PRODUCT',?,'dosbox_pure',?,?,?,'SINGLE_FILE','{}','READY','/recent',zeroblob(32),'FINISHED',?,?,?,?,?,1)
`, launchID, gameID, target.ProviderID, target.TargetID, target.BundleSHA256,
			now+60_000, now, now+120_000, now, now)
		mustExecHTTPTest(t, transaction, `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,started_at_ms,
last_heartbeat_at_ms,ended_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,'local',?,?,?,?,60000,1,'FINISHED',1,?,?)
`, playID, launchID, gameID, now-int64(index+1)*1_000, now, now, now, now)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

type gameDetailSeed struct {
	now                                           int64
	videoPayload, screenshot                      []byte
	videoMetadata                                 blobstore.Metadata
	latestLaunchID, videoBlobID, screenshotBlobID string
}

func seedGameDetailMedia(
	t *testing.T, server *Server, transaction *sql.Tx,
	gameID, _, _, coverBlobID, coverAssetID, videoAssetID string,
	fixture *gameDetailSeed,
) {
	now := fixture.now
	var err error
	mustExecHTTPTest(t, transaction, `
PRAGMA defer_foreign_keys=ON
`)
	mustExecHTTPTest(t, transaction, `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms
) VALUES(
 ?,(SELECT id FROM platform_instances WHERE catalog_template_key='dos/dosbox_pure'),
 'Doom','D','','','','',1,1993,'IMPORT_REVIEW','review','SINGLE_FILE','IMPORT_REVIEW','review',
 '{}',?,'PUBLISHED','doom',1,?,?
)
`, gameID, strings.Repeat("0", 64), now, now)
	mustExecHTTPTest(t, transaction, `
INSERT INTO blobs(id,
sha256,
size_bytes,
md5,
sha1,
crc32,
media_type,
created_at_ms) VALUES(?,
?,
4,
?,
?,
?,
'image/png',
?)
`, coverBlobID, strings.Repeat("1", 64), strings.Repeat("2", 32), strings.Repeat("3", 40),
		strings.Repeat("4", 8), now)
	mustExecHTTPTest(t, transaction, `
INSERT INTO game_assets(id,
game_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
'COVER',
0,
600,
800,
'image/png',
?)
`, coverAssetID, gameID, coverBlobID, now)
	fixture.videoPayload = []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'}
	fixture.videoMetadata, err = server.blobs.Put(bytes.NewReader(fixture.videoPayload))
	testassert.False(t, err != nil, err)
	fixture.videoBlobID, err = blobstore.EnsureRecord(t.Context(), transaction, fixture.videoMetadata, "video/mp4", now)
	testassert.False(t, err != nil, err)
	mustExecHTTPTest(t, transaction, `
INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES(?,?,?,'VIDEO',0,NULL,NULL,'video/mp4',?)
`, videoAssetID, gameID, fixture.videoBlobID, now)
}

func seedGameDetailRuntime(
	t *testing.T, server *Server, transaction *sql.Tx,
	gameID, _, variantID, _ string, saveStateID string,
	fixture *gameDetailSeed,
) {
	now := fixture.now
	mustExecHTTPTest(t, transaction, `
INSERT INTO dos_entries(game_id,
normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe) VALUES(?,
 'GAMES/DOOM.EXE',
'GAMES/DOOM.EXE',
'EXE',
0,
1,
1),
(?,
 'SETUP%.BAT',
'SETUP%.BAT',
'BAT',
1,
1,
0)
`, gameID, gameID)
	requireHTTPTestRuntimeTarget(t, transaction, "dosbox_pure")
	target, err := testsupport.LookupRuntimeTarget(t.Context(), transaction, "dosbox_pure")
	testassert.False(t, err != nil, err)
	mustExecHTTPTest(t, transaction, `
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,status,
 compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms
) VALUES(?,?,?, ?,?,NULL,9001,'READY','READY','{}',NULL,1,?,?)
`, variantID, gameID, "dosbox_pure", target.ProviderID, target.TargetID, now, now)
	fixture.screenshot = []byte("retrom-save-fixture.screenshot")
	screenshotMetadata, err := server.blobs.Put(bytes.NewReader(fixture.screenshot))
	testassert.False(t, err != nil, err)
	fixture.screenshotBlobID, err = blobstore.EnsureRecord(t.Context(), transaction, screenshotMetadata, "image/png", now)
	testassert.False(t, err != nil, err)
	sourceLaunchID := uuid.NewString()
	mustExecHTTPTest(t, transaction, `
INSERT INTO launch_sessions(id,profile_id,purpose,game_id,core_id,
provider_id,target_id,bundle_sha256,content_kind,dependency_snapshot_json,compatibility_code,return_to,
credential_sha256,state,bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version)
VALUES(?,'local','PRODUCT',?,'dosbox_pure',?,?,?,'SINGLE_FILE','{}','READY','/',zeroblob(32),'FINISHED',?,?,?, ?,?,1)
`, sourceLaunchID, gameID, target.ProviderID, target.TargetID, target.BundleSHA256,
		now+60_000, now, now+120_000, now, now)
	payloadDigest := sha256.Sum256(fixture.screenshot)
	mustExecHTTPTest(t, transaction, `
INSERT INTO save_states(
 id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,payload_size_bytes,
 screenshot_blob_id,source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms
) VALUES(?,'local',?,'test-checkpoint-v1',?,?,?,?,?,'入口存档',180000,1,?,?,NULL)
`, saveStateID, gameID, fixture.screenshotBlobID, hex.EncodeToString(payloadDigest[:]), len(fixture.screenshot),
		fixture.screenshotBlobID, sourceLaunchID, now, now)
	for index := 0; index < 8; index++ {
		mustExecHTTPTest(t, transaction, `
INSERT INTO save_states(
 id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,payload_size_bytes,
 screenshot_blob_id,source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms
) VALUES(?,'local',?,'test-checkpoint-v1',?,?,?,?,?,?,60000,1,?,?,NULL)
`, uuid.NewString(), gameID, fixture.screenshotBlobID, hex.EncodeToString(payloadDigest[:]), len(fixture.screenshot),
			fixture.screenshotBlobID, sourceLaunchID, fmt.Sprintf("额外存档 %d", index+1),
			now+int64(index+1), now+int64(index+1))
	}
	for index, duration := range []int64{120_000, 240_000} {
		launchID, playID := uuid.NewString(), uuid.NewString()
		fixture.latestLaunchID = launchID
		mustExecHTTPTest(t, transaction, `
INSERT INTO launch_sessions(id,profile_id,purpose,game_id,core_id,
provider_id,target_id,bundle_sha256,content_kind,dependency_snapshot_json,compatibility_code,return_to,
credential_sha256,state,bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version)
VALUES(?,'local','PRODUCT',?,'dosbox_pure',?,?,?,'SINGLE_FILE','{}','READY','/',zeroblob(32),'FINISHED',?,?,?, ?,?,1)
`, launchID, gameID, target.ProviderID, target.TargetID, target.BundleSHA256,
			now+60_000, now+int64(index), now+120_000, now, now+int64(index))
		mustExecHTTPTest(t, transaction, `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,started_at_ms,
last_heartbeat_at_ms,ended_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,'local',?,?,?,?,?,1,'FINISHED',1,?,?)
`, playID, launchID, gameID, now-20_000+int64(index)*10_000, now, now, duration,
			now, now+int64(10-index))
	}
	mustCommitHTTPTest(t, transaction)
}
