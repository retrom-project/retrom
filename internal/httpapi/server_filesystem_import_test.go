package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/testsupport"
)

func TestServerFilesystemImportWithoutConfiguration(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	seedFilesystemImportCatalog(t, server)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "游戏目录"), 0o700); err != nil {
		t.Fatal(err)
	}
	selectedPath := strings.TrimPrefix(filepath.ToSlash(source), "/")
	get := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	roots := get("/api/v1/admin/server-import-roots")
	if roots.Code != http.StatusOK || !strings.Contains(roots.Body.String(), `"id":"filesystem"`) {
		t.Fatalf("filesystem unavailable without configuration: %d %s", roots.Code, roots.Body.String())
	}
	directoriesURL := "/api/v1/admin/server-import-roots/filesystem/directories?path=" + url.QueryEscape(selectedPath)
	directories := get(directoriesURL)
	if directories.Code != http.StatusOK || !strings.Contains(directories.Body.String(), selectedPath+"/游戏目录") {
		t.Fatalf("unconfigured directory cannot be browsed: %d %s", directories.Code, directories.Body.String())
	}
	for _, endpoint := range []string{"pegasus-imports", "emulationstation-imports", "server-imports"} {
		t.Run(endpoint, func(t *testing.T) {
			body := map[string]any{"rootId": "filesystem", "sourceRelativePath": selectedPath}
			if endpoint == "server-imports" {
				body["kind"] = "BIOS_DIRECTORY"
				body["replaceIfBetter"] = false
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/"+endpoint, strings.NewReader(string(encoded)))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", uuid.NewString())
			setCSRFCredentials(request, cookie, csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("unconfigured directory cannot be imported: %d %s", response.Code, response.Body.String())
			}
		})
	}
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: uuid.NewString(), ProfileID: uuid.NewString(), Username: "member", Role: "USER",
	}}
	if response := get(directoriesURL); response.Code != http.StatusForbidden {
		t.Fatalf("member browsed filesystem: %d", response.Code)
	}
	server.authenticator = serverImportRejectAuthenticator{}
	if response := get(directoriesURL); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous user browsed filesystem: %d", response.Code)
	}
}

func seedFilesystemImportCatalog(t *testing.T, server *Server) {
	t.Helper()
	requireHTTPTestRuntimeTarget(t, server.database, "mgba")
	target, err := testsupport.LookupRuntimeTarget(t.Context(), server.database, "mgba")
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.database.ExecContext(t.Context(), `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,logical_name,
requirement_mode,catalog_digest,size_bytes,md5,source_url,source_version,enabled,version,
created_at_ms,updated_at_ms,delivery_kind)
VALUES('filesystem-requirement','mgba',?,?,'STATIC','bios.bin','REQUIRED',lower(hex(zeroblob(32))),
7,'c0a53b8a2b3c6f7a7f6e1fcbf9f99f15','https://example.invalid/bios','filesystem-v1',1,1,1,1,'BIOS_BUNDLE')
`, target.ProviderID, target.TargetID)
	if err != nil {
		t.Fatal(err)
	}
}
