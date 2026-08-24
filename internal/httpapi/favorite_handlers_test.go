package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/config"
	"retrom/internal/testassert"
)

type favoriteHTTPAuthenticator struct {
	principal authn.Principal
	token     string
}

func (authenticator favoriteHTTPAuthenticator) Authenticate(context.Context, string) (accounts.Session, error) {
	return accounts.Session{Principal: authenticator.principal, CookieToken: authenticator.token}, nil
}

func favoriteHTTPCredentials() (*http.Cookie, string, string) {
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = byte(index + 1)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("retrom-csrf-v1"))
	return &http.Cookie{Name: "retrom_session", Value: token, Path: "/"},
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), token
}

const (
	favoriteHTTPGameA    = "01980000-0000-7000-8000-00000000f401"
	favoriteHTTPGameB    = "01980000-0000-7000-8000-00000000f402"
	favoriteHTTPUserA    = "01980000-0000-7000-8000-00000000b401"
	favoriteHTTPProfileA = "01980000-0000-7000-8000-00000000a401"
	favoriteHTTPUserB    = "01980000-0000-7000-8000-00000000b402"
	favoriteHTTPProfileB = "01980000-0000-7000-8000-00000000a402"
)

func seedFavoriteHTTPPrincipal(t *testing.T, server *Server, profileID, userID, username string) authn.Principal {
	t.Helper()
	if _, err := server.database.ExecContext(context.Background(),
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Favorite player',1000)`, profileID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO users(id,profile_id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'Favorite player','USER','ENABLED',1,1,1000,1000)
`, userID, profileID, username); err != nil {
		t.Fatal(err)
	}
	return authn.Principal{
		UserID: userID, ProfileID: profileID, Username: username,
		DisplayName: "Favorite player", Role: "USER", SessionID: uuid.NewString(),
	}
}

func seedFavoriteHTTPGame(t *testing.T, server *Server, gameID, suffix, title string) {
	t.Helper()
	metadataID := "01980000-0000-7000-8000-00000000d4" + suffix
	contentID := "01980000-0000-7000-8000-00000000e4" + suffix
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(context.Background(), "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO game_metadata_revisions(
  id,game_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,'F','','','','',NULL,1994,'ADMIN_EDIT',NULL,1000)
`, metadataID, gameID, title); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?,'ADMIN_REPLACE','favorite-http-test','[]',?,1000)
`, contentID, gameID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),'PUBLISHED',?,?,lower(?),1,1000,1000)
`, gameID, metadataID, contentID, title); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func favoriteHTTPRequest(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf, method, path, body string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("X-Retrom-Csrf", csrf)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestFavoriteHTTPContractLifecycleReplayIsolationAndProjection(t *testing.T) {
	server := newTestServer(t)
	server.config.Mode = config.ModeTest
	seedFavoriteHTTPGame(t, server, favoriteHTTPGameA, "01", "Favorite Alpha")
	seedFavoriteHTTPGame(t, server, favoriteHTTPGameB, "02", "Favorite Beta")
	principalA := seedFavoriteHTTPPrincipal(t, server, favoriteHTTPProfileA, favoriteHTTPUserA, "favorite.owner")
	principalB := seedFavoriteHTTPPrincipal(t, server, favoriteHTTPProfileB, favoriteHTTPUserB, "favorite.other")
	cookie, csrf, sessionToken := favoriteHTTPCredentials()
	server.authenticator = favoriteHTTPAuthenticator{principal: principalA, token: sessionToken}
	handler := server.Handler()

	for _, gameID := range []string{favoriteHTTPGameA, favoriteHTTPGameB} {
		response := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPut,
			"/api/v1/favorites/"+gameID, `{}`, nil)
		testassert.Falsef(t, testassert.Any(func() bool { return response.Code != http.StatusOK }, func() bool { return !strings.Contains(response.Body.String(), `"gameId":"`+gameID+`"`) }, func() bool { return response.Header().Get("Cache-Control") != "private, no-store" }), "favorite %s = %d headers=%v body=%s", gameID, response.Code, response.Header(), response.Body.String())
	}

	firstPage := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet,
		"/api/v1/favorites?sort=FAVORITED_DESC&limit=1", "", nil)
	var page struct {
		Items []struct {
			GameID string `json:"gameId"`
		} `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(firstPage.Body.Bytes(), &page); err != nil || firstPage.Code != http.StatusOK ||
		len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("first page = %d %s, error=%v", firstPage.Code, firstPage.Body.String(), err)
	}
	secondPage := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet,
		"/api/v1/favorites?sort=FAVORITED_DESC&limit=1&cursor="+*page.NextCursor, "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return secondPage.Code != http.StatusOK }, func() bool { return strings.Contains(secondPage.Body.String(), `"gameId":"`+page.Items[0].GameID+`"`) }), "second page = %d %s", secondPage.Code, secondPage.Body.String())
	cursorMismatch := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet,
		"/api/v1/favorites?sort=TITLE_ASC&limit=1&cursor="+*page.NextCursor, "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return cursorMismatch.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(cursorMismatch.Body.String(), `"code":"INVALID_CURSOR"`) }), "cursor mismatch = %d %s", cursorMismatch.Code, cursorMismatch.Body.String())

	createKey := uuid.NewString()
	create := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost, "/api/v1/favorite-folders",
		fmt.Sprintf(`{"name":"  想玩  ","initialGameIds":[%q]}`, favoriteHTTPGameA),
		map[string]string{"Idempotency-Key": createKey})
	var folder struct {
		FolderID string `json:"folderId"`
		Name     string `json:"name"`
		Version  int64  `json:"version"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &folder); err != nil || create.Code != http.StatusCreated ||
		folder.Name != "想玩" || create.Header().Get("Location") != "/api/v1/favorite-folders/"+folder.FolderID ||
		create.Header().Get("ETag") != `"v1"` {
		t.Fatalf("create folder = %d headers=%v body=%s error=%v", create.Code, create.Header(), create.Body.String(), err)
	}
	replay := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost, "/api/v1/favorite-folders",
		fmt.Sprintf(`{"name":"  想玩  ","initialGameIds":[%q]}`, favoriteHTTPGameA),
		map[string]string{"Idempotency-Key": createKey})
	testassert.Falsef(t, testassert.Any(func() bool { return replay.Code != create.Code }, func() bool { return replay.Body.String() != create.Body.String() }, func() bool { return replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" }), "create replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	reused := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost, "/api/v1/favorite-folders",
		`{"name":"不同请求","initialGameIds":[]}`, map[string]string{"Idempotency-Key": createKey})
	testassert.Falsef(t, testassert.Any(func() bool { return reused.Code != http.StatusConflict }, func() bool { return !strings.Contains(reused.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) }), "reused key = %d %s", reused.Code, reused.Body.String())

	replace := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPut,
		"/api/v1/favorites/"+favoriteHTTPGameB+"/folders", fmt.Sprintf(`{"folderIds":[%q]}`, folder.FolderID), nil)
	testassert.Falsef(t, testassert.Any(func() bool { return replace.Code != http.StatusOK }, func() bool { return !strings.Contains(replace.Body.String(), folder.FolderID) }), "replace membership = %d %s", replace.Code, replace.Body.String())
	missingETag := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPatch,
		"/api/v1/favorite-folders/"+folder.FolderID, `{"name":"待通关"}`,
		map[string]string{"Idempotency-Key": uuid.NewString()})
	testassert.Falsef(t, testassert.Any(func() bool { return missingETag.Code != http.StatusPreconditionRequired }, func() bool { return !strings.Contains(missingETag.Body.String(), `"code":"PRECONDITION_REQUIRED"`) }), "missing etag = %d %s", missingETag.Code, missingETag.Body.String())
	rename := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPatch,
		"/api/v1/favorite-folders/"+folder.FolderID, `{"name":"待通关"}`,
		map[string]string{"Idempotency-Key": uuid.NewString(), "If-Match": `"v1"`})
	testassert.Falsef(t, testassert.Any(func() bool { return rename.Code != http.StatusOK }, func() bool { return rename.Header().Get("ETag") != `"v2"` }, func() bool { return !strings.Contains(rename.Body.String(), `"name":"待通关"`) }), "rename folder = %d headers=%v body=%s", rename.Code, rename.Header(), rename.Body.String())
	stale := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPatch,
		"/api/v1/favorite-folders/"+folder.FolderID, `{"name":"过期修改"}`,
		map[string]string{"Idempotency-Key": uuid.NewString(), "If-Match": `"v1"`})
	testassert.Falsef(t, testassert.Any(func() bool { return stale.Code != http.StatusPreconditionFailed }, func() bool { return !strings.Contains(stale.Body.String(), `"code":"RESOURCE_VERSION_CONFLICT"`) }), "stale folder = %d %s", stale.Code, stale.Body.String())

	server.authenticator = favoriteHTTPAuthenticator{principal: principalB, token: sessionToken}
	foreignCursor := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet,
		"/api/v1/favorites?sort=FAVORITED_DESC&limit=1&cursor="+*page.NextCursor, "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return foreignCursor.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(foreignCursor.Body.String(), `"code":"INVALID_CURSOR"`) }), "foreign cursor = %d %s", foreignCursor.Code, foreignCursor.Body.String())
	independentKey := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost,
		"/api/v1/favorite-folders", `{"name":"Test 私有","initialGameIds":[]}`,
		map[string]string{"Idempotency-Key": createKey})
	testassert.Falsef(t, testassert.Any(func() bool { return independentKey.Code != http.StatusCreated }, func() bool { return !strings.Contains(independentKey.Body.String(), `"name":"Test 私有"`) }), "cross-principal idempotency = %d %s", independentKey.Code, independentKey.Body.String())
	otherList := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet, "/api/v1/favorites", "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return otherList.Code != http.StatusOK }, func() bool { return !strings.Contains(otherList.Body.String(), `"favoriteCount":0`) }, func() bool { return strings.Contains(otherList.Body.String(), favoriteHTTPGameA) }), "other profile list = %d %s", otherList.Code, otherList.Body.String())
	foreignFolder := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPatch,
		"/api/v1/favorite-folders/"+folder.FolderID, `{"name":"越权"}`,
		map[string]string{"Idempotency-Key": uuid.NewString(), "If-Match": `"v2"`})
	testassert.Falsef(t, testassert.Any(func() bool { return foreignFolder.Code != http.StatusNotFound }, func() bool {
		return !strings.Contains(foreignFolder.Body.String(), `"code":"FAVORITE_FOLDER_NOT_FOUND"`)
	}), "foreign folder = %d %s", foreignFolder.Code, foreignFolder.Body.String())
	server.authenticator = favoriteHTTPAuthenticator{principal: principalA, token: sessionToken}

	removeKey := uuid.NewString()
	unfavorite := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost, "/api/v1/favorites/unfavorite",
		fmt.Sprintf(`{"gameIds":[%q]}`, favoriteHTTPGameA), map[string]string{"Idempotency-Key": removeKey})
	var removed struct {
		Items []struct {
			GameID    string   `json:"gameId"`
			FolderIDs []string `json:"folderIds"`
		} `json:"items"`
	}
	if err := json.Unmarshal(unfavorite.Body.Bytes(), &removed); err != nil || unfavorite.Code != http.StatusOK ||
		len(removed.Items) != 1 || len(removed.Items[0].FolderIDs) != 1 {
		t.Fatalf("unfavorite = %d %s, error=%v", unfavorite.Code, unfavorite.Body.String(), err)
	}
	restoreBody, err := json.Marshal(map[string]any{"items": removed.Items})
	testassert.False(t, err != nil, err)
	restore := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost, "/api/v1/favorites/restore",
		string(restoreBody), map[string]string{"Idempotency-Key": uuid.NewString()})
	testassert.Falsef(t, testassert.Any(func() bool { return restore.Code != http.StatusOK }, func() bool { return !strings.Contains(restore.Body.String(), favoriteHTTPGameA) }), "restore = %d %s", restore.Code, restore.Body.String())

	gameList := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet, "/api/v1/games?limit=100", "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return gameList.Code != http.StatusOK }, func() bool { return !strings.Contains(gameList.Body.String(), `"favorite":{"favoritedAtMs":`) }, func() bool { return !strings.Contains(gameList.Body.String(), folder.FolderID) }), "game favorite projection = %d %s", gameList.Code, gameList.Body.String())

	deleteFolder := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodDelete,
		"/api/v1/favorite-folders/"+folder.FolderID, `{}`,
		map[string]string{"Idempotency-Key": uuid.NewString(), "If-Match": `"v2"`})
	testassert.Falsef(t, deleteFolder.Code != http.StatusNoContent, "delete folder = %d %s", deleteFolder.Code, deleteFolder.Body.String())
	remaining := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet, "/api/v1/favorites?scope=UNCATEGORIZED", "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return remaining.Code != http.StatusOK }, func() bool { return !strings.Contains(remaining.Body.String(), favoriteHTTPGameA) }, func() bool { return !strings.Contains(remaining.Body.String(), favoriteHTTPGameB) }), "favorites after folder delete = %d %s", remaining.Code, remaining.Body.String())
}

func TestFavoriteHTTPRejectsAnonymousUnsafeAndNonStrictRequests(t *testing.T) {
	server := newTestServer(t)
	server.config.Mode = config.ModeTest
	seedFavoriteHTTPGame(t, server, favoriteHTTPGameA, "01", "Favorite Alpha")
	principal := seedFavoriteHTTPPrincipal(t, server, favoriteHTTPProfileA, favoriteHTTPUserA, "favorite.validation")
	cookie, csrf, sessionToken := favoriteHTTPCredentials()
	server.authenticator = favoriteHTTPAuthenticator{principal: principal, token: sessionToken}
	handler := server.Handler()

	server.authenticator = nil
	anonymous := favoriteHTTPRequest(t, handler, nil, "", http.MethodGet, "/api/v1/favorites", "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return anonymous.Code != http.StatusUnauthorized }, func() bool { return !strings.Contains(anonymous.Body.String(), `"code":"AUTHENTICATION_REQUIRED"`) }), "anonymous favorites = %d %s", anonymous.Code, anonymous.Body.String())
	server.authenticator = favoriteHTTPAuthenticator{principal: principal, token: sessionToken}
	unknownField := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPut,
		"/api/v1/favorites/"+favoriteHTTPGameA, `{"unknown":true}`, nil)
	testassert.Falsef(t, testassert.Any(func() bool { return unknownField.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(unknownField.Body.String(), `"code":"INVALID_REQUEST"`) }), "unknown favorite field = %d %s", unknownField.Code, unknownField.Body.String())
	missingKey := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodPost,
		"/api/v1/favorite-folders", `{"name":"想玩","initialGameIds":[]}`, nil)
	testassert.Falsef(t, testassert.Any(func() bool { return missingKey.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(missingKey.Body.String(), `"code":"INVALID_IDEMPOTENCY_KEY"`) }), "missing idempotency key = %d %s", missingKey.Code, missingKey.Body.String())
	badQuery := favoriteHTTPRequest(t, handler, cookie, csrf, http.MethodGet,
		"/api/v1/favorites?scope=FOLDER", "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return badQuery.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(badQuery.Body.String(), `"code":"INVALID_QUERY"`) }), "invalid favorite query = %d %s", badQuery.Code, badQuery.Body.String())

	unsafe := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/favorites/"+favoriteHTTPGameA, strings.NewReader(`{}`))
	unsafe.Header.Set("Content-Type", "application/json")
	unsafe.AddCookie(cookie)
	unsafe.Header.Set("X-Retrom-Csrf", csrf)
	unsafe.Header.Set("Origin", "https://attacker.invalid")
	unsafe.Header.Set("Sec-Fetch-Site", "cross-site")
	unsafeResponse := httptest.NewRecorder()
	handler.ServeHTTP(unsafeResponse, unsafe)
	testassert.Falsef(t, testassert.Any(func() bool { return unsafeResponse.Code != http.StatusForbidden }, func() bool { return unsafeResponse.Header().Get("Access-Control-Allow-Origin") != "" }), "unsafe favorite write = %d headers=%v body=%s", unsafeResponse.Code, unsafeResponse.Header(), unsafeResponse.Body.String())
}
