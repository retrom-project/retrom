//go:build integration

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"retrom/internal/libraryimport"
	"retrom/internal/rpgmaker/isolation"
	"retrom/internal/runtimelaunch"
	"retrom/internal/testsupport"
)

func TestNativeReviewIsolationServesFrozenMVAndMZResources(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"mv", "mz"} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			server, itemID := newNativeReviewIsolationFixture(t, engine)
			preview, cookie := createCheckpointPreviewHTTP(t, server, itemID, nil)
			origin, isolatedCookie := bootstrapNativeReviewHTTP(t, server, preview.PreviewID, cookie)
			paths := []string{"/__retrom/entry", "/__retrom/bridge.js", "/__retrom/project/js/main.js", "/__retrom/project/data/system.JSON"}
			for _, path := range paths {
				t.Run(path, func(t *testing.T) { assertNativeReviewResourceMethods(t, server, origin+path, path, isolatedCookie) })
			}
			otherCookie := assertNativeReviewFrozenRestore(t, server, itemID, preview.PreviewID, cookie)
			assertNativeReviewIsolationDenied(t, server, origin, paths, isolatedCookie, otherCookie)
			assertNativeReviewIsolationRevoked(t, server, origin, paths, isolatedCookie)
		})
	}
}

func assertNativeReviewResourceMethods(t *testing.T, server *Server, target, path string, cookie *http.Cookie) {
	t.Helper()
	for _, method := range []string{"GET", "HEAD"} {
		response := requestReviewArchiveHTTP(t, server, target, method, cookie, "")
		if method == "HEAD" && !strings.HasPrefix(path, "/__retrom/project/") {
			if response.Code != http.StatusNotFound {
				t.Fatalf("GET-only native route admitted HEAD %s: %d", path, response.Code)
			}
			continue
		}
		if response.Code != http.StatusOK || method == "HEAD" && response.Body.Len() != 0 {
			t.Fatalf("native review %s %s = %d %s", method, path, response.Code, response.Body.String())
		}
		if method == "GET" && path == "/__retrom/entry" && !strings.Contains(response.Body.String(), `<base href="/__retrom/project/">`) {
			t.Fatal("entry did not install the isolated project base")
		}
	}
}

func assertNativeReviewFrozenRestore(t *testing.T, server *Server, itemID, previewID string, cookie *http.Cookie) *http.Cookie {
	t.Helper()
	configuration := requestReviewCheckpointHTTP(t, server, previewID, cookie, "GET", "checkpoint-status", nil, "")
	var declaration struct {
		CheckpointFormat string `json:"checkpointFormat"`
	}
	if configuration.Code != 200 || json.Unmarshal(configuration.Body.Bytes(), &declaration) != nil || declaration.CheckpointFormat == "" {
		t.Fatalf("native checkpoint status: %d %s", configuration.Code, configuration.Body.String())
	}
	save := func(value string) {
		body, kind := checkpointHTTPForm(t, declaration.CheckpointFormat, value)
		response := requestReviewCheckpointHTTP(t, server, previewID, cookie, "POST", "save-states", body, kind)
		if response.Code != 201 {
			t.Fatalf("native checkpoint save = %d %s", response.Code, response.Body.String())
		}
	}
	save("frozen-B")
	restored, restoreCookie := createCheckpointPreviewHTTP(t, server, itemID, &previewID)
	restoreOrigin, restoreIsolatedCookie := bootstrapNativeReviewHTTP(t, server, restored.PreviewID, restoreCookie)
	save("later-C")
	state := requestReviewArchiveHTTP(t, server, restoreOrigin+"/__retrom/restore-payload", "GET", restoreIsolatedCookie, "")
	if state.Code != 200 || state.Body.String() != "frozen-B" {
		t.Fatalf("isolated frozen restore = %d %s", state.Code, state.Body.String())
	}
	return restoreIsolatedCookie
}

func assertNativeReviewIsolationRevoked(t *testing.T, server *Server, origin string, paths []string, cookie *http.Cookie) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), "POST", origin+"/__retrom/cleanup", nil)
	request.AddCookie(cookie)
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("native cleanup = %d %s", response.Code, response.Body.String())
	}
	for _, path := range paths {
		if response := requestReviewArchiveHTTP(t, server, origin+path, "GET", cookie, ""); response.Code != 404 {
			t.Fatalf("revoked native review %s = %d", path, response.Code)
		}
	}
}
func newNativeReviewIsolationFixture(t *testing.T, engine string) (*Server, string) {
	t.Helper()
	server := newTestServer(t)
	const template = "http://{launchId}.rpg.localhost:3000"
	server.launcher.WithRPGRuntimeOriginTemplate(template)
	server.rpgIsolation = isolation.New(server.database, template, time.Now)
	active, manifests, err := testsupport.RuntimeProviderInputs(t.Context(), server.database)
	if err != nil {
		t.Fatal(err)
	}
	manifest := manifests["retrom-runtime"]
	for index := range manifest.Targets {
		if manifest.Targets[index].ID == "rpgmaker-"+engine {
			manifest.Targets[index].AssetPaths = append(manifest.Targets[index].AssetPaths, "native/bridge.js")
		}
	}
	manifests["retrom-runtime"] = manifest
	builder, err := runtimelaunch.NewBuilder(active, manifests)
	if err != nil {
		t.Fatal(err)
	}
	server.WithRuntimeProvider(server.dependencies.RuntimeCatalog, builder, http.NotFoundHandler())
	// The installed Provider handler is not part of this HTTP authorization test.
	server.runtimeProvider = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/native/bridge.js") {
			t.Errorf("undeclared bridge asset: %s", r.URL.Path)
		}
		if _, err := fmt.Fprint(w, "// installed bridge"); err != nil {
			t.Error(err)
		}
	})
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	prefix, version := "rpg", "1.6.2"
	if engine == "mz" {
		prefix, version = "rmmz", "1.9.0"
	}
	scripts := []string{"js/" + prefix + "_core.js", "js/" + prefix + "_managers.js", "js/" + prefix + "_objects.js", "js/" + prefix + "_scenes.js", "js/" + prefix + "_sprites.js", "js/" + prefix + "_windows.js", "js/plugins.js", "js/main.js"}
	if engine == "mz" {
		scripts = append(scripts, "js/libs/localforage.min.js")
	}
	files := make([]rpgMakerHTTPFixtureFile, 0, len(scripts)+2)
	files = append(files, rpgMakerHTTPFixtureFile{path: "project/data/System.json", contents: []byte(`{"gameTitle":"Native HTTP fixture"}`)})
	entry := "<!doctype html><html><head>"
	for index, script := range scripts {
		content := "// native HTTP fixture"
		if index == 0 {
			content = `Utils.RPGMAKER_VERSION = "` + version + `";`
		}
		files = append(files, rpgMakerHTTPFixtureFile{path: "project/" + script, contents: []byte(content)})
		entry += `<script src="` + script + `"></script>`
	}
	files = append(files, rpgMakerHTTPFixtureFile{path: "project/index.html", contents: []byte(entry + "</head><body></body></html>")})
	uploadID := completeRPGMakerHTTPUpload(t, t.Context(), server, files)
	created, err := server.importer.Create(t.Context(), libraryimport.CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, server.database, "rpgmaker/rpgmaker"),
		MetadataProvider: "NONE", ContentMode: "RPG_MAKER_PROJECT",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := server.database.QueryRowContext(t.Context(), `SELECT id FROM import_items WHERE import_job_id=?`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return server, itemID
}

func bootstrapNativeReviewHTTP(t *testing.T, server *Server, previewID string, cookie *http.Cookie) (string, *http.Cookie) {
	t.Helper()
	response := requestReviewCheckpointHTTP(t, server, previewID, cookie, "GET", "config", nil, "")
	var envelope map[string]any
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("native config = %d %s", response.Code, response.Body.String())
	}
	resource := testsupport.RuntimeEnvelopeResource(t, envelope, "game")
	origin, ok := resource["origin"].(string)
	if !ok || origin == "" {
		t.Fatal("native resource omitted its isolated origin")
	}
	body, _ := json.Marshal(map[string]any{"ticket": resource["bootstrapTicket"]})
	request := httptest.NewRequestWithContext(t.Context(), "POST", origin+"/__retrom/bootstrap", bytes.NewReader(body))
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("native bootstrap = %d %s", response.Code, response.Body.String())
	}
	for _, issued := range response.Result().Cookies() {
		if issued.Name == rpgRuntimeCookieName {
			return origin, issued
		}
	}
	t.Fatal("missing isolated preview credential")
	return "", nil
}

func assertNativeReviewIsolationDenied(t *testing.T, server *Server, origin string, paths []string, cookie, otherCookie *http.Cookie) {
	t.Helper()
	for _, path := range paths {
		for _, supplied := range []*http.Cookie{nil, otherCookie} {
			if response := requestReviewArchiveHTTP(t, server, origin+path, "GET", supplied, ""); response.Code != 404 {
				t.Fatalf("unowned native resource %s = %d", path, response.Code)
			}
		}
	}
	for _, path := range []string{"/__retrom/project/index.html", "/__retrom/project/plugin.exe", "/__retrom/project/js/missing.js"} {
		if response := requestReviewArchiveHTTP(t, server, origin+path, "GET", cookie, ""); response.Code != 404 {
			t.Fatalf("unsafe native resource %s = %d", path, response.Code)
		}
	}
	request := httptest.NewRequestWithContext(t.Context(), "GET", origin+"/__retrom/project/js/main.js", nil)
	request.AddCookie(cookie)
	request.Header.Set("Sec-Fetch-Dest", "serviceworker")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("native service worker request = %d", response.Code)
	}
}
