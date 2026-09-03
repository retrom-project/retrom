package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestJobEventStreamUsesTransactionalSnapshotAndGlobalCursor(t *testing.T) {
	server := newTestServer(t)
	now := time.Now().UnixMilli()
	targetID := "01980000-0000-7000-8000-000000000081"
	otherID := "01980000-0000-7000-8000-000000000082"
	for index, jobID := range []string{targetID, otherID} {
		dedupe := strings.Repeat(string(rune('a'+index)), 64)
		if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
finished_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'VARIANT_REVALIDATE',
?,
1,
'{}',
0,
'SUCCEEDED',
1,
2,
?,
?,
?,
?)
`, jobID, jobID, dedupe, now, now, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES
(?,
'GAME_VARIANT',
?,
'STARTED',
'{"source":"target-old"}',
?),
(?,
'GAME_VARIANT',
?,
'SUCCEEDED',
'{"source":"other"}',
?)
`, targetID, targetID, now, otherID, otherID, now); err != nil {
		t.Fatal(err)
	}

	snapshot := httptest.NewRecorder()
	server.Handler().
		ServeHTTP(snapshot, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/jobs/"+targetID+"/events", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return snapshot.Code != http.StatusOK }, func() bool { return !strings.Contains(snapshot.Body.String(), "id: 2\nevent: snapshot") }, func() bool { return !strings.Contains(snapshot.Body.String(), `"state":"SUCCEEDED"`) }, func() bool { return strings.Contains(snapshot.Body.String(), "target-old") }), "snapshot stream = %d %s", snapshot.Code, snapshot.Body.String())
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'SUCCEEDED',
'{"source":"target-new"}',
?)
`, targetID, targetID, now); err != nil {
		t.Fatal(err)
	}
	reconnectRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/jobs/"+targetID+"/events", nil)
	reconnectRequest.Header.Set("Last-Event-ID", "2")
	reconnect := httptest.NewRecorder()
	server.Handler().ServeHTTP(reconnect, reconnectRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return reconnect.Code != http.StatusOK }, func() bool { return !strings.Contains(reconnect.Body.String(), "id: 3\nevent: succeeded") }, func() bool { return !strings.Contains(reconnect.Body.String(), "target-new") }, func() bool { return strings.Contains(reconnect.Body.String(), "event: snapshot") }), "reconnected stream = %d %s", reconnect.Code, reconnect.Body.String())
	invalidRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/jobs/"+targetID+"/events", nil)
	invalidRequest.Header.Set("Last-Event-ID", "4")
	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, invalidRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return invalid.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(invalid.Body.String(), `"code":"INVALID_EVENT_CURSOR"`) }), "invalid cursor = %d %s", invalid.Code, invalid.Body.String())

	runningID := "01980000-0000-7000-8000-000000000083"
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'VARIANT_REVALIDATE',
?,
1,
'{}',
0,
'RUNNING',
1,
2,
?,
?,
?)
`, runningID, runningID, strings.Repeat("c", 64), now, now, now); err != nil {
		t.Fatal(err)
	}
	server.sseHeartbeat = 5 * time.Millisecond
	heartbeatContext, cancelHeartbeat := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelHeartbeat()
	heartbeatRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/jobs/"+runningID+"/events", nil).
		WithContext(heartbeatContext)
	heartbeat := &cancelOnMarkerRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		marker:           []byte(": heartbeat\n\n"),
		cancel:           cancelHeartbeat,
	}
	server.Handler().ServeHTTP(heartbeat, heartbeatRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return heartbeat.Code != http.StatusOK }, func() bool { return !strings.Contains(heartbeat.Body.String(), ": heartbeat\n\n") }), "heartbeat stream = %d %s", heartbeat.Code, heartbeat.Body.String())
}

type cancelOnMarkerRecorder struct {
	*httptest.ResponseRecorder
	marker []byte
	cancel context.CancelFunc
}

func (recorder *cancelOnMarkerRecorder) Write(contents []byte) (int, error) {
	written, err := recorder.ResponseRecorder.Write(contents)
	if bytes.Contains(contents, recorder.marker) {
		recorder.cancel()
	}
	return written, err
}

func setCSRFCredentials(request *http.Request, cookie *http.Cookie, token string) {
	request.AddCookie(cookie)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-Retrom-Csrf", token)
}

func TestParseETag(t *testing.T) {
	t.Parallel()
	if version, err := ParseETag(`"v1"`); err != nil || version != 1 {
		t.Fatalf("ParseETag minimum = %d, %v", version, err)
	}
	if version, err := ParseETag(`"v42"`); err != nil || version != 42 {
		t.Fatalf("ParseETag = %d, %v", version, err)
	}
	for _, invalid := range []string{"v42", `W/"v42"`, `"v0"`, `"v042"`, `"x1"`} {
		if _, err := ParseETag(invalid); err == nil {
			t.Fatalf("ParseETag(%q) succeeded", invalid)
		}
	}
}

func TestDecodeJSONRejectsDuplicateInvalidUTF8AndDeepValues(t *testing.T) {
	t.Parallel()
	for name, contents := range map[string][]byte{
		"duplicate":    []byte(`{"name":"one","name":"two"}`),
		"invalid utf8": {'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
		"trailing":     []byte(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(contents))
			request.Header.Set("Content-Type", "application/json")
			if err := decodeJSON(httptest.NewRecorder(), request, &map[string]any{}, 4096); err == nil {
				t.Fatal("decodeJSON accepted malformed JSON")
			}
		})
	}
	deep := bytes.Repeat([]byte{'['}, 65)
	deep = append(deep, '0')
	deep = append(deep, bytes.Repeat([]byte{']'}, 65)...)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(deep))
	request.Header.Set("Content-Type", "application/json")
	if err := decodeJSON(httptest.NewRecorder(), request, &[]any{}, 4096); err == nil {
		t.Fatal("decodeJSON accepted depth 65")
	}
}

func TestGameMetadataPatchDistinguishesNullFromAbsent(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", strings.NewReader(`{"players":null,"releaseYear":1993}`))
	request.Header.Set("Content-Type", "application/json")
	var body patchGameRequest
	if err := decodeJSON(httptest.NewRecorder(), request, &body, 4096); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return !body.Players.Present }, func() bool { return body.Players.Value != nil }, func() bool { return !body.ReleaseYear.Present }, func() bool { return body.ReleaseYear.Value == nil }, func() bool { return *body.ReleaseYear.Value != 1993 }, func() bool { return !validPatchGame(body, time.Now()) }), "nullable patch = %#v", body)
	testassert.False(t, validPatchGame(patchGameRequest{}, time.Now()), "empty metadata patch accepted")
}

func newTestServer(t *testing.T) *Server {
	return newTestServerWithPlatformFixtures(t, true, []string{"4.2.3"})
}

func newRecommendationTestServer(t *testing.T) *Server {
	t.Helper()
	server := newTestServerWithPlatformFixtures(t, false, []string{"4.2.3", "4.3.0-pre"})
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.Now()); err != nil {
		t.Fatalf("bootstrap recommendation dependencies: %v", err)
	}
	server.startupReady.Store(true)
	return server
}

func newTestServerWithPlatformFixtures(t *testing.T, seedDirectories bool, versions []string) *Server {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	testassert.Falsef(t, err != nil, "repository root: %v", err)
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "open database: %v", err)
	if seedDirectories {
		if err := testsupport.SeedPlatformInstances(context.Background(), database.SQL); err != nil {
			t.Fatalf("seed platform instances: %v", err)
		}
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','测试玩家',0)
`); err != nil {
		t.Fatalf("seed service fixture profile: %v", err)
	}
	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000009999','local','test-admin','Test Admin','ADMIN','ENABLED',0,0)
`); err != nil {
		t.Fatalf("seed service fixture user: %v", err)
	}
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), versions, "4.2.3")
	testassert.Falsef(t, err != nil, "load dependencies: %v", err)
	if err := testsupport.SeedRuntimeProviders(context.Background(), database.SQL, dependencySet.RuntimeCatalog); err != nil {
		t.Fatalf("seed runtime providers: %v", err)
	}
	origin, _ := url.Parse("http://localhost:3000")
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	testassert.Falsef(t, err != nil, "open blobs: %v", err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.Falsef(t, err != nil, "create credentials: %v", err)
	server := New(
		config.Config{PublicOrigin: origin, ActiveEJSVersion: "4.2.3", DataDir: dataDir},
		database.SQL,
		dependencySet,
		blobs,
		credentials,
		testAuthenticator{},
		nil,
		time.Now,
	).WithReadinessDatabase(database.ReadOnly)
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(context.Background(), database.SQL)
	testassert.Falsef(t, err != nil, "build runtime Provider fixture: %v", err)
	server.WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder, http.NotFoundHandler())
	// General HTTP contract tests exercise handlers, not the asynchronous DAT
	// readiness lifecycle. Readiness-specific tests explicitly clear this bit.
	server.startupReady.Store(true)
	t.Cleanup(server.Close)
	return server
}

type testAuthenticator struct{}

type fixedAuthenticator struct {
	Principal authn.Principal
	Err       error
}

func testSessionCredentials() (*http.Cookie, string) {
	return &http.Cookie{Name: "retrom_test", Value: "test-only", Path: "/"}, "test-only"
}

func (testAuthenticator) Authenticate(context.Context, string) (accounts.Session, error) {
	principal := authn.Principal{
		UserID: "01980000-0000-7000-8000-000000009999", ProfileID: "local", Username: "test-admin",
		DisplayName: "Test Admin", Role: "ADMIN", SessionID: "01980000-0000-7000-8000-000000009998",
	}
	return accounts.Session{Principal: principal, CookieToken: "test-only"}, nil
}

func (authenticator fixedAuthenticator) Authenticate(context.Context, string) (accounts.Session, error) {
	return accounts.Session{Principal: authenticator.Principal, CookieToken: "test-only"}, authenticator.Err
}
