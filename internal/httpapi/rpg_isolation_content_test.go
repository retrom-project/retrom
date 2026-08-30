package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"retrom/internal/config"
	"retrom/internal/rpgmaker/isolation"
)

func TestRPGFrameDocumentsCanBeEmbeddedOnlyThroughTheirCSP(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	setRPGFrameDocumentPolicy(recorder)
	if value := recorder.Header().Get("Cross-Origin-Resource-Policy"); value != "cross-origin" {
		t.Fatalf("Cross-Origin-Resource-Policy = %q", value)
	}
}

func TestRPGEntryCSPAllowsOnlySameOriginOrBlobWorkers(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	setRPGEntryCSP(recorder, "test-nonce", "https://retrom.example")
	want := "default-src 'self' data: blob:; script-src 'self' 'nonce-test-nonce' 'unsafe-eval' blob:; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; " +
		"font-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; frame-src 'none'; " +
		"object-src 'none'; base-uri 'self'; form-action 'none'; frame-ancestors https://retrom.example"
	if got := recorder.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("entry CSP = %q, want %q", got, want)
	}
}

func TestTransformRPGEntryReplacesProjectBaseAndLoadsBridgeFirst(t *testing.T) {
	t.Parallel()
	original := []byte(`<!doctype html><html><head data-title=">"><base href="game/"><title>Game</title></head><body><script src="js/main.js"></script></body></html>`)
	transformed, err := transformRPGEntry(original)
	if err != nil {
		t.Fatal(err)
	}
	value := string(transformed)
	if strings.Contains(value, `href="game/"`) || strings.Count(value, "<base") != 1 {
		t.Fatalf("project base was not replaced: %s", value)
	}
	base := strings.Index(value, `<base href="/__retrom/project/">`)
	bridge := strings.Index(value, `<script src="/__retrom/bridge.js"></script>`)
	project := strings.Index(value, `<script src="js/main.js"></script>`)
	if base < 0 || bridge < base || project < bridge {
		t.Fatalf("runtime injection order is unsafe: %s", value)
	}
}

func TestTransformRPGEntryRejectsMalformedOrNonUTF8Documents(t *testing.T) {
	t.Parallel()
	for _, contents := range [][]byte{
		[]byte(`<html><body></body></html>`),
		[]byte(`<html><head title="></head><body></body></html>`),
		{0xff, 0xfe},
	} {
		if _, err := transformRPGEntry(contents); err == nil {
			t.Fatalf("invalid entry accepted: %q", contents)
		}
	}
}

func TestTransformTyranoScriptEntryInstallsProjectBaseAndBridge(t *testing.T) {
	t.Parallel()
	original := []byte(`<!doctype html><html><head><base href="./"><script src="tyrano/tyrano.js"></script></head></html>`)
	transformed, err := transformIsolatedEntry(
		original, "/__retrom/tyranoscript/project/", "/__retrom/tyranoscript/bridge.js",
	)
	if err != nil {
		t.Fatal(err)
	}
	value := string(transformed)
	base := strings.Index(value, `<base href="/__retrom/tyranoscript/project/">`)
	bridge := strings.Index(value, `<script src="/__retrom/tyranoscript/bridge.js"></script>`)
	project := strings.Index(value, `<script src="tyrano/tyrano.js"></script>`)
	if base < 0 || bridge < base || project < bridge || strings.Count(value, "<base") != 1 {
		t.Fatalf("TyranoScript injection order is unsafe: %s", value)
	}
}

func TestTyranoScriptProjectMIMEIsExplicit(t *testing.T) {
	t.Parallel()
	for _, logicalName := range []string{
		"data/scenario/first.ks", "data/system/Config.tjs", "tyrano/tyrano.js", "data/image/title.webp",
	} {
		if _, ok := tyranoScriptProjectMIME(logicalName); !ok {
			t.Fatalf("valid TyranoScript project file rejected: %s", logicalName)
		}
	}
	for _, logicalName := range []string{"index.php", "launch.exe", "plugin.node", "archive.zip"} {
		if _, ok := tyranoScriptProjectMIME(logicalName); ok {
			t.Fatalf("unsafe TyranoScript project file accepted: %s", logicalName)
		}
	}
}

func TestTyranoScriptProjectPathSupportsEngineAbsoluteResources(t *testing.T) {
	t.Parallel()
	accepted := map[string]string{
		"/__retrom/tyranoscript/project/data/scenario/first.ks": "data/scenario/first.ks",
		"/__retrom/tyranoscript/data/bgimage/title.jpg":         "data/bgimage/title.jpg",
		"/__retrom/tyranoscript/tyrano/html/menu.html":          "tyrano/html/menu.html",
		"/data/bgimage/title.jpg":                               "data/bgimage/title.jpg",
		"/tyrano/html/menu.html":                                "tyrano/html/menu.html",
	}
	for requestPath, expected := range accepted {
		logicalName, ok := tyranoScriptProjectLogicalName(requestPath)
		if !ok || logicalName != expected {
			t.Fatalf("TyranoScript project path %q = %q/%t", requestPath, logicalName, ok)
		}
	}
	for _, requestPath := range []string{
		"/__retrom/tyranoscript/bridge.js", "/__retrom/tyranoscript/bootstrap",
		"/__retrom/tyranoscript/entry", "/__retrom/tyranoscript/arbitrary/plugin.js",
		"/__retrom/secret.js", "/../secret.js", "/launch.exe", "/arbitrary/plugin.js",
	} {
		if logicalName, ok := tyranoScriptProjectLogicalName(requestPath); ok {
			t.Fatalf("unsafe TyranoScript project path %q accepted as %q", requestPath, logicalName)
		}
	}
}

func TestRPGRuntimeRequestTargetAllowsOnlyTyranoScriptNumericCacheBusters(t *testing.T) {
	t.Parallel()
	accepted := []string{
		"https://runtime.example/__retrom/tyranoscript/project/data/system/Config.tjs",
		"https://runtime.example/__retrom/tyranoscript/project/data/system/Config.tjs?_=1788123731998",
		"https://runtime.example/data/scenario/title.ks?_=1788123731998",
	}
	for _, target := range accepted {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if !validRPGRuntimeRequestTarget(request) {
			t.Fatalf("valid runtime target rejected: %s", target)
		}
	}
	for _, target := range []string{
		"https://runtime.example/__retrom/entry?_=1",
		"https://runtime.example/__retrom/tyranoscript/project/data/system/Config.tjs?cache=1",
		"https://runtime.example/__retrom/tyranoscript/project/data/system/Config.tjs?_=1&_=2",
		"https://runtime.example/__retrom/tyranoscript/project/data/system/Config.tjs?_=not-a-number",
		"https://runtime.example/__retrom/tyranoscript/project/data/system/Config.tjs?_=123456789012345678901",
	} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if validRPGRuntimeRequestTarget(request) {
			t.Fatalf("unsafe runtime target accepted: %s", target)
		}
	}
}

func TestNativeProjectAllowlistRejectsHTMLTraversalAndUnknownExecutionTypes(t *testing.T) {
	t.Parallel()
	for _, logicalName := range []string{"js/main.js", "data/System.json", "audio/bgm/theme.ogg", "img/encrypted.rpgmvp"} {
		if !validNativeProjectPath(logicalName) {
			t.Fatalf("valid project path rejected: %s", logicalName)
		}
		if _, ok := nativeProjectMIME(logicalName); !ok {
			t.Fatalf("valid project MIME rejected: %s", logicalName)
		}
	}
	for _, logicalName := range []string{"index.html", "../secret.js", "/js/main.js", `js\main.js`, "plugin.exe", "asset.svg"} {
		_, mimeAllowed := nativeProjectMIME(logicalName)
		if validNativeProjectPath(logicalName) && mimeAllowed {
			t.Fatalf("unsafe project resource accepted: %s", logicalName)
		}
	}
}

func TestNativeProjectServiceWorkerDestinationIsDenied(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__retrom/project/js/worker.js", nil)
	request.Header.Set("Sec-Fetch-Dest", "serviceworker")
	if !isRPGServiceWorkerRequest(request) {
		t.Fatal("service worker destination was not recognized")
	}
	request.Header.Set("Sec-Fetch-Dest", "worker")
	if isRPGServiceWorkerRequest(request) {
		t.Fatal("ordinary worker destination was rejected")
	}
}

func TestBootstrapPageReusesOnlyAuthenticatedRuntimeCapability(t *testing.T) {
	t.Parallel()
	database, service, nowMS, launchID, origin, ticket := newBootstrapReloadFixture(t)
	credential, _, err := service.ConsumeTicket(context.Background(), launchID, origin, ticket)
	if err != nil {
		t.Fatal(err)
	}
	publicOrigin, err := url.Parse("https://retrom.example")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		config: config.Config{PublicOrigin: publicOrigin}, database: database,
		rpgIsolation: service, now: func() time.Time { return time.UnixMilli(*nowMS) },
	}
	access := isolation.Access{LaunchID: launchID, Origin: origin}

	authorized := bootstrapPageRequest(t, server, access, credential)
	if authorized.Code != http.StatusSeeOther || authorized.Header().Get("Location") != "/__retrom/entry" ||
		authorized.Header().Get("Cache-Control") != "private, no-store" ||
		authorized.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Fatalf("authorized reload = %d headers=%v body=%s", authorized.Code, authorized.Header(), authorized.Body.String())
	}
	for _, denied := range []struct {
		name       string
		access     isolation.Access
		credential string
	}{
		{name: "missing cookie", access: access},
		{name: "forged cookie", access: access, credential: "forged"},
		{name: "wrong host origin", access: isolation.Access{LaunchID: launchID, Origin: "https://wrong.example"}, credential: credential},
	} {
		t.Run(denied.name, func(t *testing.T) {
			response := bootstrapPageRequest(t, server, denied.access, denied.credential)
			if response.Code != http.StatusGone || response.Header().Get("Location") != "" ||
				!strings.Contains(response.Body.String(), `"code":"RPG_RUNTIME_BOOTSTRAP_EXPIRED"`) {
				t.Fatalf("denied reload = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
			}
		})
	}
	*nowMS += 120_000
	expired := bootstrapPageRequest(t, server, access, credential)
	if expired.Code != http.StatusGone || expired.Header().Get("Location") != "" {
		t.Fatalf("expired reload = %d headers=%v body=%s", expired.Code, expired.Header(), expired.Body.String())
	}
}

func bootstrapPageRequest(
	t *testing.T,
	server *Server,
	access isolation.Access,
	credential string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, access.Origin+"/__retrom/bootstrap", nil)
	if credential != "" {
		request.AddCookie(&http.Cookie{Name: rpgRuntimeCookieName, Value: credential})
	}
	response := httptest.NewRecorder()
	server.rpgBootstrapPage(response, request, access)
	return response
}

func newBootstrapReloadFixture(
	t *testing.T,
) (*sql.DB, *isolation.Service, *int64, string, string, string) {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
CREATE TABLE core_artifacts(
 id TEXT PRIMARY KEY,runtime_family TEXT,runtime_adapter_kind TEXT,available_for_launch INTEGER
);
CREATE TABLE launch_sessions(
 id TEXT PRIMARY KEY,profile_id TEXT,core_artifact_id TEXT,state TEXT,hard_expires_at_ms INTEGER
);
CREATE TABLE review_preview_sessions(
 id TEXT PRIMARY KEY,core_artifact_id TEXT,state TEXT,hard_expires_at_ms INTEGER
);
CREATE TABLE isolated_runtime_bootstrap_tickets(
 ticket_sha256 BLOB,launch_id TEXT,preview_id TEXT,profile_id TEXT,expected_origin TEXT,
 expires_at_ms INTEGER,consumed_at_ms INTEGER
);
CREATE TABLE isolated_runtime_capabilities(
 credential_sha256 BLOB,launch_id TEXT,preview_id TEXT,profile_id TEXT,expected_origin TEXT,
 issued_at_ms INTEGER,expires_at_ms INTEGER,revoked_at_ms INTEGER
);`); err != nil {
		t.Fatal(err)
	}
	const launchID = "01980000-0000-7000-8000-000000000091"
	const origin = "https://01980000-0000-7000-8000-000000000091.rpg-runtime.example"
	nowMS := int64(10_000)
	ticketBytes := bytes.Repeat([]byte{0x3c}, 32)
	ticket := base64.RawURLEncoding.EncodeToString(ticketBytes)
	ticketDigest := sha256.Sum256(ticketBytes)
	for _, statement := range []struct {
		query     string
		arguments []any
	}{
		{`INSERT INTO core_artifacts VALUES('artifact','RPGMAKER','NATIVE_WEB',1)`, nil},
		{`INSERT INTO launch_sessions VALUES(?,'profile','artifact','ACTIVE',?)`, []any{launchID, nowMS + 120_000}},
		{`INSERT INTO isolated_runtime_bootstrap_tickets VALUES(?,?,NULL,'profile',?,?,NULL)`, []any{ticketDigest[:], launchID, origin, nowMS + 60_000}},
	} {
		if _, err := database.ExecContext(ctx, statement.query, statement.arguments...); err != nil {
			t.Fatal(err)
		}
	}
	service := isolation.New(database, "https://{launchId}.rpg-runtime.example", func() time.Time {
		return time.UnixMilli(nowMS)
	})
	return database, service, &nowMS, launchID, origin, ticket
}
