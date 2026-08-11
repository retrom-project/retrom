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
	if _, err := server.database.Exec(`
INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,
source_commit,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES('server-http-artifact','mgba','4.2.3','server-http','WASM','data/server-http.js',1,lower(hex(zeroblob(32))),
NULL,'{}','{}',1,1,1,1);
INSERT INTO bios_requirements(id,core_id,core_artifact_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES('server-http-requirement','mgba','server-http-artifact','STATIC',NULL,'bios.bin','REQUIRED',NULL,NULL,
lower(hex(zeroblob(32))),7,'c0a53b8a2b3c6f7a7f6e1fcbf9f99f15',NULL,NULL,
'https://example.invalid/bios','server-http-v1',1,1,1,1,'BIOS_BUNDLE',NULL)
`); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	get := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	roots := get("/api/v1/admin/server-import-roots")
	if roots.Code != http.StatusOK || !strings.Contains(roots.Body.String(), `"id":"bios-root"`) ||
		strings.Contains(roots.Body.String(), root) {
		t.Fatalf("root projection = %d %s", roots.Code, roots.Body.String())
	}
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
	if secondPage.Code != http.StatusOK || strings.Contains(secondPage.Body.String(), directoryPage.Items[0].RelativePath) {
		t.Fatalf("directory second page = %d %s", secondPage.Code, secondPage.Body.String())
	}
	crossPath := get("/api/v1/admin/server-import-roots/bios-root/directories?path=A%20BIOS&limit=1&cursor=" + *directoryPage.NextCursor)
	if crossPath.Code != http.StatusBadRequest || !strings.Contains(crossPath.Body.String(), "INVALID_CURSOR") {
		t.Fatalf("cross-path cursor = %d %s", crossPath.Code, crossPath.Body.String())
	}
	escape := get("/api/v1/admin/server-import-roots/bios-root/directories?path=..%2Fescape")
	if escape.Code != http.StatusBadRequest || strings.Contains(escape.Body.String(), root) {
		t.Fatalf("escape path = %d %s", escape.Code, escape.Body.String())
	}

	server.authenticator = serverImportRejectAuthenticator{}
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-import-roots", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous roots = %d %s", anonymous.Code, anonymous.Body.String())
	}
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: uuid.NewString(), ProfileID: uuid.NewString(), Username: "member", DisplayName: "Member", Role: "USER",
	}}
	member := get("/api/v1/admin/server-import-roots")
	if member.Code != http.StatusForbidden || !strings.Contains(member.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("member roots = %d %s", member.Code, member.Body.String())
	}
	server.authenticator = testAuthenticator{}

	post := func(key, body string, ifMatch string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/server-imports", strings.NewReader(body))
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
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown create field = %d %s", invalid.Code, invalid.Body.String())
	}
	key := uuid.NewString()
	body := `{"kind":"BIOS_DIRECTORY","rootId":"bios-root","sourceRelativePath":"","replaceIfBetter":false}`
	created := post(key, body, "")
	replayed := post(key, body, "")
	if created.Code != http.StatusAccepted || replayed.Code != http.StatusAccepted ||
		created.Body.String() != replayed.Body.String() || replayed.Header().Get("X-Retrom-Idempotent-Replay") != "true" ||
		created.Header().Get("Location") == "" || created.Header().Get("ETag") != `"v1"` {
		t.Fatalf("create/replay = %d/%d %s/%s headers=%v", created.Code, replayed.Code, created.Body.String(), replayed.Body.String(), replayed.Header())
	}
	conflict := post(key, `{"kind":"BIOS_DIRECTORY","rootId":"bios-root","sourceRelativePath":"A BIOS","replaceIfBetter":false}`, "")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("idempotency conflict = %d %s", conflict.Code, conflict.Body.String())
	}
	active := post(uuid.NewString(), body, "")
	if active.Code != http.StatusConflict || !strings.Contains(active.Body.String(), "SERVER_BIOS_IMPORT_ACTIVE") {
		t.Fatalf("active conflict = %d %s", active.Code, active.Body.String())
	}

	var createdBody serverimport.Summary
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	cancelRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/server-imports/%s/cancel", createdBody.ID), strings.NewReader(`{"reason":"stop fixture"}`))
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("If-Match", `"v1"`)
	cancelRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(cancelRequest, cookie, csrf)
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, cancelRequest)
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"state":"CANCELLED"`) {
		t.Fatalf("queued cancel = %d %s", cancelled.Code, cancelled.Body.String())
	}
}
