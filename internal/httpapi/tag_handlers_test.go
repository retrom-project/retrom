package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/tagging"
	"retrom/internal/testassert"
)

func tagHTTPRequest(
	t *testing.T,
	handler http.Handler,
	auth *accountHTTPAuth,
	method, target, body string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
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
	testassert.Falsef(t, unauthorized.Code != http.StatusUnauthorized, "anonymous list = %d %s", unauthorized.Code, unauthorized.Body.String())
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
	testassert.Falsef(t, testassert.Any(func() bool { return replayed.Code != created.Code }, func() bool { return replayed.Body.String() != created.Body.String() }, func() bool { return replayed.Header().Get("ETag") != created.Header().Get("ETag") }), "create replay = %d %s etag=%q", replayed.Code, replayed.Body.String(), replayed.Header().Get("ETag"))
	reusedKey := createWithKey("不同请求", createKey)
	testassert.Falsef(t, testassert.Any(func() bool { return reusedKey.Code != http.StatusConflict }, func() bool { return !strings.Contains(reusedKey.Body.String(), "IDEMPOTENCY_KEY_REUSED") }), "create key conflict = %d %s", reusedKey.Code, reusedKey.Body.String())
	conflict := create("co-OP")
	testassert.Falsef(t, testassert.Any(func() bool { return conflict.Code != http.StatusConflict }, func() bool { return !strings.Contains(conflict.Body.String(), "TAG_NAME_CONFLICT") }), "duplicate = %d %s", conflict.Code, conflict.Body.String())

	unknownTagID := "01980000-0000-7000-8000-00000000c999"
	invalidAssignment := tagHTTPRequest(t, handler, &auth, http.MethodPut, "/api/v1/admin/games/"+gameID+"/tags",
		`{"tagIds":["`+unknownTagID+`"]}`, map[string]string{
			"If-Match": `"v1"`, "Idempotency-Key": uuid.NewString(),
		})
	testassert.Falsef(t, testassert.Any(func() bool { return invalidAssignment.Code != http.StatusUnprocessableEntity }, func() bool {
		return !strings.Contains(invalidAssignment.Body.String(), `"code":"TAG_REFERENCE_INVALID"`)
	}, func() bool { return !strings.Contains(invalidAssignment.Body.String(), unknownTagID) }), "invalid assignment = %d %s", invalidAssignment.Code, invalidAssignment.Body.String())
	assigned := tagHTTPRequest(t, handler, &auth, http.MethodPut, "/api/v1/admin/games/"+gameID+"/tags",
		`{"tagIds":["`+tag.TagID+`"]}`, map[string]string{
			"If-Match": `"v1"`, "Idempotency-Key": uuid.NewString(),
		})
	testassert.Falsef(t, testassert.Any(func() bool { return assigned.Code != http.StatusOK }, func() bool { return assigned.Header().Get("ETag") != `"v2"` }, func() bool { return !strings.Contains(assigned.Body.String(), `"name":"Co-op"`) }), "assignment = %d %s", assigned.Code, assigned.Body.String())
	for _, target := range []string{
		"/api/v1/games?q=co-op",
		"/api/v1/games?tagId=" + tag.TagID,
		"/api/v1/admin/games?q=CO-OP",
		"/api/v1/admin/games?tagId=" + tag.TagID,
	} {
		response := tagHTTPRequest(t, handler, &auth, http.MethodGet, target, "", nil)
		testassert.Falsef(t, testassert.Any(func() bool { return response.Code != http.StatusOK }, func() bool { return !strings.Contains(response.Body.String(), gameID) }, func() bool { return !strings.Contains(response.Body.String(), `"name":"Co-op"`) }), "search %s = %d %s", target, response.Code, response.Body.String())
	}

	detail := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/admin/tags/"+tag.TagID, "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return detail.Code != http.StatusOK }, func() bool { return detail.Header().Get("ETag") != `"v2"` }, func() bool { return !strings.Contains(detail.Body.String(), `"publishedGameCount":1`) }), "detail = %d %s etag=%q", detail.Code, detail.Body.String(), detail.Header().Get("ETag"))
	renamed := tagHTTPRequest(t, handler, &auth, http.MethodPatch, "/api/v1/admin/tags/"+tag.TagID,
		`{"name":"合作"}`, map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()})
	testassert.Falsef(t, testassert.Any(func() bool { return renamed.Code != http.StatusOK }, func() bool { return renamed.Header().Get("ETag") != `"v3"` }), "rename = %d %s", renamed.Code, renamed.Body.String())
	oldSearch := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/games?q=co-op", "", nil)
	newSearch := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/games?q=%E5%90%88%E4%BD%9C", "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return strings.Contains(oldSearch.Body.String(), gameID) }, func() bool { return !strings.Contains(newSearch.Body.String(), gameID) }), "renamed search old=%s new=%s", oldSearch.Body.String(), newSearch.Body.String())

	deleted := tagHTTPRequest(t, handler, &auth, http.MethodDelete, "/api/v1/admin/tags/"+tag.TagID,
		`{"confirmName":"合作"}`, map[string]string{"If-Match": `"v3"`, "Idempotency-Key": uuid.NewString()})
	testassert.Falsef(t, testassert.Any(func() bool { return deleted.Code != http.StatusNoContent }, func() bool { return deleted.Header().Get("ETag") != `"v4"` }), "delete = %d %s etag=%q", deleted.Code, deleted.Body.String(), deleted.Header().Get("ETag"))
	staleOwner := tagHTTPRequest(t, handler, &auth, http.MethodPut, "/api/v1/admin/games/"+gameID+"/tags",
		`{"tagIds":[]}`, map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()})
	testassert.Falsef(t, testassert.Any(func() bool { return staleOwner.Code != http.StatusConflict }, func() bool { return !strings.Contains(staleOwner.Body.String(), "VERSION_CONFLICT") }), "stale owner = %d %s", staleOwner.Code, staleOwner.Body.String())
	deletedSearch := tagHTTPRequest(t, handler, &auth, http.MethodGet, "/api/v1/games?tagId="+tag.TagID, "", nil)
	testassert.Falsef(t, testassert.Any(func() bool { return deletedSearch.Code != http.StatusOK }, func() bool { return strings.Contains(deletedSearch.Body.String(), gameID) }), "deleted search = %d %s", deletedSearch.Code, deletedSearch.Body.String())
	recreated := create("合作")
	testassert.Falsef(t, testassert.Any(func() bool { return recreated.Code != http.StatusCreated }, func() bool { return strings.Contains(recreated.Body.String(), tag.TagID) }), "name reuse = %d %s", recreated.Code, recreated.Body.String())

	var audits int
	if err := server.database.QueryRowContext(context.Background(), `
SELECT count(*) FROM audit_events WHERE action IN ('TAG_CREATED','TAG_RENAMED','TAG_DELETED','GAME_TAGS_REPLACED')
`).Scan(&audits); err != nil || audits != 5 {
		t.Fatalf("audit count = %d, %v", audits, err)
	}
}

func mustJSONText(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	testassert.False(t, err != nil, err)
	return string(encoded)
}
