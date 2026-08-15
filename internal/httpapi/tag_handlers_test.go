package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/tagging"
)

func tagHTTPRequest(
	t *testing.T,
	handler http.Handler,
	auth *accountHTTPAuth,
	method, target, body string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Origin", "http://localhost:3000")
	}
	if auth != nil {
		request.AddCookie(auth.cookie)
		request.Header.Set("X-Retrom-Csrf", auth.csrf)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestTagHTTPCRUDGameAssignmentSearchAndDeleteInvalidation(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	const gameID = "01980000-0000-7000-8000-00000000f434"
	seedFavoriteHTTPGame(t, server, gameID, "34", "Search Fixture")

	unauthorized := tagHTTPRequest(t, handler, nil, http.MethodGet, "/api/v1/admin/tags", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	auth := accountHTTPLogin(t, handler)
	createWithKey := func(name, key string) *httptest.ResponseRecorder {
		return tagHTTPRequest(t, handler, &auth, http.MethodPost, "/api/v1/admin/tags",
			`{"name":`+mustJSONText(t, name)+`}`, map[string]string{"Idempotency-Key": key})
	}
	create := func(name string) *httptest.ResponseRecorder { return createWithKey(name, uuid.NewString()) }

	createKey := uuid.NewString()
	created := createWithKey("  Co-op  ", createKey)
	var tag tagging.AdminItem
	if err := json.Unmarshal(created.Body.Bytes(), &tag); err != nil || created.Code != http.StatusCreated ||
		tag.Name != "Co-op" || created.Header().Get("ETag") != `"v1"` {
		t.Fatalf("create = %d %#v %s error=%v", created.Code, tag, created.Body.String(), err)
	}
	replayed := createWithKey("  Co-op  ", createKey)
	if replayed.Code != created.Code || replayed.Body.String() != created.Body.String() || replayed.Header().Get("ETag") != created.Header().Get("ETag") {
		t.Fatalf("create replay = %d %s etag=%q", replayed.Code, replayed.Body.String(), replayed.Header().Get("ETag"))
	}
	reusedKey := createWithKey("不同请求", createKey)
	if reusedKey.Code != http.StatusConflict || !strings.Contains(reusedKey.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("create key conflict = %d %s", reusedKey.Code, reusedKey.Body.String())
	}
	conflict := create("co-OP")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "TAG_NAME_CONFLICT") {
		t.Fatalf("duplicate = %d %s", conflict.Code, conflict.Body.String())
	}

	unknownTagID := "01980000-0000-7000-8000-00000000c999"
	invalidAssignment := tagHTTPRequest(t, handler, &auth, http.MethodPut, "/api/v1/admin/games/"+gameID+"/tags",
		`{"tagIds":["`+unknownTagID+`"]}`, map[string]string{
			"If-Match": `"v1"`, "Idempotency-Key": uuid.NewString(),
		})
	if invalidAssignment.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(invalidAssignment.Body.String(), `"code":"TAG_REFERENCE_INVALID"`) ||
		!strings.Contains(invalidAssignment.Body.String(), unknownTagID) {
		t.Fatalf("invalid assignment = %d %s", invalidAssignment.Code, invalidAssignment.Body.String())
	}
	assigned := tagHTTPRequest(t, handler, &auth, http.MethodPut, "/api/v1/admin/games/"+gameID+"/tags",
		`{"tagIds":["`+tag.TagID+`"]}`, map[string]string{
			"If-Match": `"v1"`, "Idempotency-Key": uuid.NewString(),
		})
	if assigned.Code != http.StatusOK || assigned.Header().Get("ETag") != `"v2"` ||
		!strings.Contains(assigned.Body.String(), `"name":"Co-op"`) {
		t.Fatalf("assignment = %d %s", assigned.Code, assigned.Body.String())
	}
	for _, target := range []string{
		"/api/v1/games?q=co-op",
		"/api/v1/games?tagId=" + tag.TagID,
		"/api/v1/admin/games?q=CO-OP",
		"/api/v1/admin/games?tagId=" + tag.TagID,
	} {
		response := tagHTTPRequest(t, handler, &auth, http.MethodGet, target, "", nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), gameID) ||
			!strings.Contains(response.Body.String(), `"name":"Co-op"`) {
			t.Fatalf("search %s = %d %s", target, response.Code, response.Body.String())
		}
	}

	detail := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/admin/tags/"+tag.TagID, "", nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"v2"` ||
		!strings.Contains(detail.Body.String(), `"publishedGameCount":1`) {
		t.Fatalf("detail = %d %s etag=%q", detail.Code, detail.Body.String(), detail.Header().Get("ETag"))
	}
	renamed := tagHTTPRequest(t, handler, &auth, http.MethodPatch, "/api/v1/admin/tags/"+tag.TagID,
		`{"name":"合作"}`, map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()})
	if renamed.Code != http.StatusOK || renamed.Header().Get("ETag") != `"v3"` {
		t.Fatalf("rename = %d %s", renamed.Code, renamed.Body.String())
	}
	oldSearch := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/games?q=co-op", "", nil)
	newSearch := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/games?q=%E5%90%88%E4%BD%9C", "", nil)
	if strings.Contains(oldSearch.Body.String(), gameID) || !strings.Contains(newSearch.Body.String(), gameID) {
		t.Fatalf("renamed search old=%s new=%s", oldSearch.Body.String(), newSearch.Body.String())
	}

	deleted := tagHTTPRequest(t, handler, &auth, http.MethodDelete, "/api/v1/admin/tags/"+tag.TagID,
		`{"confirmName":"合作"}`, map[string]string{"If-Match": `"v3"`, "Idempotency-Key": uuid.NewString()})
	if deleted.Code != http.StatusNoContent || deleted.Header().Get("ETag") != `"v4"` {
		t.Fatalf("delete = %d %s etag=%q", deleted.Code, deleted.Body.String(), deleted.Header().Get("ETag"))
	}
	staleOwner := tagHTTPRequest(t, handler, &auth, http.MethodPut, "/api/v1/admin/games/"+gameID+"/tags",
		`{"tagIds":[]}`, map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()})
	if staleOwner.Code != http.StatusConflict || !strings.Contains(staleOwner.Body.String(), "VERSION_CONFLICT") {
		t.Fatalf("stale owner = %d %s", staleOwner.Code, staleOwner.Body.String())
	}
	deletedSearch := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/games?tagId="+tag.TagID, "", nil)
	if deletedSearch.Code != http.StatusOK || strings.Contains(deletedSearch.Body.String(), gameID) {
		t.Fatalf("deleted search = %d %s", deletedSearch.Code, deletedSearch.Body.String())
	}
	recreated := create("合作")
	if recreated.Code != http.StatusCreated || strings.Contains(recreated.Body.String(), tag.TagID) {
		t.Fatalf("name reuse = %d %s", recreated.Code, recreated.Body.String())
	}

	var audits int
	if err := server.database.QueryRow(`
SELECT count(*) FROM audit_events WHERE action IN ('TAG_CREATED','TAG_RENAMED','TAG_DELETED','GAME_TAGS_REPLACED')
`).Scan(&audits); err != nil || audits != 5 {
		t.Fatalf("audit count = %d, %v", audits, err)
	}
}

func mustJSONText(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
