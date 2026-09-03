package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/config"
	"retrom/internal/serverimport"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type serverImportRejectAuthenticator struct{}

func (serverImportRejectAuthenticator) Authenticate(context.Context, string) (accounts.Session, error) {
	return accounts.Session{}, accounts.ErrAuthenticationNeeded
}

func TestServerImportHTTPRootBoundaryAuthorizationAndIdempotency(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	server.serverImports.Close()
	root := t.TempDir()
	for _, name := range []string{"A BIOS", "B BIOS"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bios.bin"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	server.serverImports = serverimport.New(
		server.database, server.blobs, server.firmware, server.credentials,
		[]config.ServerImportRoot{{ID: "bios-root", Label: "BIOS Root", Path: root, CanonicalPath: root}},
		time.Now,
	)
	t.Cleanup(server.serverImports.Close)
	requireHTTPTestRuntimeTarget(t, server.database, "mgba")
	target, err := testsupport.LookupRuntimeTarget(t.Context(), server.database, "mgba")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,target_contract_sha256,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES('server-http-requirement','mgba',?,?,?,'STATIC',NULL,'bios.bin','REQUIRED',NULL,NULL,
lower(hex(zeroblob(32))),7,'c0a53b8a2b3c6f7a7f6e1fcbf9f99f15',NULL,NULL,
'https://example.invalid/bios','server-http-v1',1,1,1,1,'BIOS_BUNDLE',NULL)
`, target.ProviderID, target.TargetID, target.TargetContractSHA256); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	get := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	roots := get("/api/v1/admin/server-import-roots")
	testassert.Falsef(t, testassert.Any(func() bool { return roots.Code != http.StatusOK }, func() bool { return !strings.Contains(roots.Body.String(), `"id":"bios-root"`) }, func() bool { return strings.Contains(roots.Body.String(), root) }), "root projection = %d %s", roots.Code, roots.Body.String())
	firstPage := get("/api/v1/admin/server-import-roots/bios-root/directories?path=&limit=1")
	var directoryPage struct {
		Items []struct {
			RelativePath string `json:"relativePath"`
		} `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(firstPage.Body.Bytes(), &directoryPage); err != nil || firstPage.Code != http.StatusOK ||
		len(directoryPage.Items) != 1 || directoryPage.NextCursor == nil || strings.Contains(firstPage.Body.String(), root) ||
		strings.Contains(firstPage.Body.String(), "escape") {
		t.Fatalf("directory page = %d %#v %s, error=%v", firstPage.Code, directoryPage, firstPage.Body.String(), err)
	}
	secondPage := get("/api/v1/admin/server-import-roots/bios-root/directories?path=&limit=1&cursor=" + *directoryPage.NextCursor)
	testassert.Falsef(t, testassert.Any(func() bool { return secondPage.Code != http.StatusOK }, func() bool { return strings.Contains(secondPage.Body.String(), directoryPage.Items[0].RelativePath) }), "directory second page = %d %s", secondPage.Code, secondPage.Body.String())
	crossPath := get("/api/v1/admin/server-import-roots/bios-root/directories?path=A%20BIOS&limit=1&cursor=" + *directoryPage.NextCursor)
	testassert.Falsef(t, testassert.Any(func() bool { return crossPath.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(crossPath.Body.String(), "INVALID_CURSOR") }), "cross-path cursor = %d %s", crossPath.Code, crossPath.Body.String())
	escape := get("/api/v1/admin/server-import-roots/bios-root/directories?path=..%2Fescape")
	testassert.Falsef(t, testassert.Any(func() bool { return escape.Code != http.StatusBadRequest }, func() bool { return strings.Contains(escape.Body.String(), root) }), "escape path = %d %s", escape.Code, escape.Body.String())

	server.authenticator = serverImportRejectAuthenticator{}
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/server-import-roots", nil))
	testassert.Falsef(t, anonymous.Code != http.StatusUnauthorized, "anonymous roots = %d %s", anonymous.Code, anonymous.Body.String())
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: uuid.NewString(), ProfileID: uuid.NewString(), Username: "member", DisplayName: "Member", Role: "USER",
	}}
	member := get("/api/v1/admin/server-import-roots")
	testassert.Falsef(t, testassert.Any(func() bool { return member.Code != http.StatusForbidden }, func() bool { return !strings.Contains(member.Body.String(), "ADMIN_REQUIRED") }), "member roots = %d %s", member.Code, member.Body.String())
	server.authenticator = testAuthenticator{}

	post := func(key, body string, ifMatch string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/server-imports", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		if ifMatch != "" {
			request.Header.Set("If-Match", ifMatch)
		}
		setCSRFCredentials(request, cookie, csrf)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	invalid := post(uuid.NewString(), `{"kind":"BIOS_DIRECTORY","rootId":"bios-root","sourceRelativePath":"","replaceIfBetter":false,"unknown":true}`, "")
	testassert.Falsef(t, invalid.Code != http.StatusBadRequest, "unknown create field = %d %s", invalid.Code, invalid.Body.String())
	key := uuid.NewString()
	body := `{"kind":"BIOS_DIRECTORY","rootId":"bios-root","sourceRelativePath":"","replaceIfBetter":false}`
	created := post(key, body, "")
	replayed := post(key, body, "")
	testassert.Falsef(t, testassert.Any(func() bool { return created.Code != http.StatusAccepted }, func() bool { return replayed.Code != http.StatusAccepted }, func() bool { return created.Body.String() != replayed.Body.String() }, func() bool { return replayed.Header().Get("X-Retrom-Idempotent-Replay") != "true" }, func() bool { return created.Header().Get("Location") == "" }, func() bool { return created.Header().Get("ETag") != `"v1"` }), "create/replay = %d/%d %s/%s headers=%v", created.Code, replayed.Code, created.Body.String(), replayed.Body.String(), replayed.Header())
	conflict := post(key, `{"kind":"BIOS_DIRECTORY","rootId":"bios-root","sourceRelativePath":"A BIOS","replaceIfBetter":false}`, "")
	testassert.Falsef(t, testassert.Any(func() bool { return conflict.Code != http.StatusConflict }, func() bool { return !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_KEY_REUSED") }), "idempotency conflict = %d %s", conflict.Code, conflict.Body.String())
	active := post(uuid.NewString(), body, "")
	testassert.Falsef(t, testassert.Any(func() bool { return active.Code != http.StatusConflict }, func() bool { return !strings.Contains(active.Body.String(), "SERVER_BIOS_IMPORT_ACTIVE") }), "active conflict = %d %s", active.Code, active.Body.String())

	var createdBody serverimport.Summary
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	cancelRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, fmt.Sprintf("/api/v1/admin/server-imports/%s/cancel", createdBody.ID), strings.NewReader(`{"reason":"stop fixture"}`))
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("If-Match", `"v1"`)
	cancelRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(cancelRequest, cookie, csrf)
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, cancelRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return cancelled.Code != http.StatusOK }, func() bool { return !strings.Contains(cancelled.Body.String(), `"state":"CANCELLED"`) }), "queued cancel = %d %s", cancelled.Code, cancelled.Body.String())
}
