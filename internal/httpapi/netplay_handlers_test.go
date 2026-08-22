package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/netplay"
	"retrom/internal/testassert"
)

type sseTestWriter struct {
	header      http.Header
	deadlines   []time.Time
	deadlineErr error
	writeErr    error
	flushErr    error
}

func (writer *sseTestWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}
func (*sseTestWriter) WriteHeader(int) {}
func (writer *sseTestWriter) Write(contents []byte) (int, error) {
	if writer.writeErr != nil {
		return 0, writer.writeErr
	}
	return len(contents), nil
}

func (writer *sseTestWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadlines = append(writer.deadlines, deadline)
	return writer.deadlineErr
}
func (writer *sseTestWriter) FlushError() error { return writer.flushErr }

func TestSSEWritesRefreshDeadlineAndPropagateBoundaryErrors(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_000_000, 0)
	server := &Server{now: func() time.Time { return now }}
	writer := &sseTestWriter{}
	if err := server.writeSSE(writer, "one"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := server.writeSSE(writer, "two"); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(writer.deadlines) != 2 }, func() bool { return !writer.deadlines[0].Equal(time.Unix(1_786_000_030, 0)) }, func() bool { return !writer.deadlines[1].Equal(time.Unix(1_786_000_150, 0)) }), "SSE deadlines = %v", writer.deadlines)

	unsupported := &sseTestWriter{deadlineErr: errors.ErrUnsupported}
	if err := server.writeSSE(unsupported, "supported fallback"); err != nil {
		t.Fatalf("unsupported deadline should degrade exactly: %v", err)
	}
	for name, failing := range map[string]*sseTestWriter{
		"deadline": {deadlineErr: errors.New("deadline failed")},
		"write":    {writeErr: errors.New("write failed")},
		"flush":    {flushErr: errors.New("flush failed")},
	} {
		if err := server.writeSSE(failing, "event"); err == nil {
			t.Fatalf("%s error was ignored", name)
		}
	}
}

func TestNetplayEventStreamDisablesProxyTransformationAndBuffering(t *testing.T) {
	t.Parallel()
	headers := make(http.Header)
	setNetplayEventStreamHeaders(headers)

	if got := headers.Get("Cache-Control"); got != "private, no-store, no-transform" {
		t.Fatalf("SSE Cache-Control = %q", got)
	}
	if got := headers.Get("Content-Encoding"); got != "identity" {
		t.Fatalf("SSE Content-Encoding = %q", got)
	}
	if got := headers.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("SSE X-Accel-Buffering = %q", got)
	}
}

func TestNetplayFeatureFlagHidesRoutesAndAuthProjection(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	t.Cleanup(server.Close)
	handler := server.Handler()
	auth := accountHTTPLogin(t, handler)

	contextRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/context", nil)
	contextRequest.AddCookie(auth.cookie)
	contextResponse := httptest.NewRecorder()
	handler.ServeHTTP(contextResponse, contextRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return contextResponse.Code != http.StatusOK }, func() bool { return !strings.Contains(contextResponse.Body.String(), `"netplayEnabled":false`) }), "disabled auth context = %d %s", contextResponse.Code, contextResponse.Body.String())

	routeRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/netplay/rooms", nil)
	routeRequest.AddCookie(auth.cookie)
	routeResponse := httptest.NewRecorder()
	handler.ServeHTTP(routeResponse, routeRequest)
	testassert.Falsef(t, routeResponse.Code != http.StatusNotFound, "disabled netplay route = %d %s", routeResponse.Code, routeResponse.Body.String())
}

func TestNetplayRoomCreateIsAuthenticatedIdempotentAndVersioned(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	server.config.NetplayEnabled = true
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	testassert.False(t, err != nil, err)
	registry, err := netplay.LoadRegistry(filepath.Join(repositoryRoot, "data"), server.dependencies)
	testassert.False(t, err != nil, err)
	credentials, err := netplay.LoadOrCreateCredentials(server.config.DataDir)
	testassert.False(t, err != nil, err)
	service := netplay.NewService(server.database, registry, credentials, netplay.Options{
		MaxActiveRooms: 16, DraftIdle: 15 * time.Minute, WaitingIdle: 30 * time.Minute, ReconnectLease: 10 * time.Second,
	}, time.Now)
	server.WithNetplay(service)
	t.Cleanup(server.Close)
	handler := server.Handler()

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/netplay/rooms", nil))
	testassert.Falsef(t, unauthenticated.Code != http.StatusUnauthorized, "anonymous room list = %d %s", unauthenticated.Code, unauthenticated.Body.String())

	auth := accountHTTPLogin(t, handler)
	key := uuid.NewString()
	sendCreate := func() *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/netplay/rooms", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("Origin", "http://localhost:3000")
		request.Header.Set("X-Retrom-Csrf", auth.csrf)
		request.AddCookie(auth.cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	created, replay := sendCreate(), sendCreate()
	testassert.Falsef(t, testassert.Any(func() bool { return created.Code != http.StatusCreated }, func() bool { return replay.Code != http.StatusCreated }, func() bool { return replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" }, func() bool { return created.Body.String() != replay.Body.String() }), "create/replay = %d %s / %d %s", created.Code, created.Body.String(), replay.Code, replay.Body.String())
	var room netplay.Room
	if err := json.Unmarshal(created.Body.Bytes(), &room); err != nil || room.State != netplay.RoomStateDraft || room.Version != 1 || room.SelfMemberID == nil || !room.Permissions.Host {
		t.Fatalf("created room = %#v, %v", room, err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return created.Header().Get("ETag") != `"v1"` }, func() bool { return created.Header().Get("Location") != "/api/v1/netplay/rooms/"+room.RoomID }), "create headers = %#v", created.Header())

	getRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/netplay/rooms/"+room.RoomID, nil)
	getRequest.AddCookie(auth.cookie)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return getResponse.Code != http.StatusOK }, func() bool { return getResponse.Header().Get("ETag") != `"v1"` }), "get room = %d %s", getResponse.Code, getResponse.Body.String())
}

func TestAcceptanceNP010NetplaySocketRejectsEveryNonExactOriginAndProtocol(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	t.Cleanup(server.Close)
	valid := func() *http.Request {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/runtime/netplay/rooms/room/socket", nil)
		request.Header.Set("Origin", server.config.PublicOrigin.String())
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("Sec-WebSocket-Protocol", netplay.WebSocketSubprotocol)
		return request
	}
	testassert.True(t, server.validNetplaySocketRequest(valid()), "contract WebSocket subprotocol was rejected")
	withoutFetchMetadata := valid()
	withoutFetchMetadata.Header.Del("Sec-Fetch-Site")
	testassert.True(t, server.validNetplaySocketRequest(withoutFetchMetadata), "browser WebSocket request without Fetch Metadata was rejected")
	tests := map[string]func(*http.Request){
		"missing origin":   func(request *http.Request) { request.Header.Del("Origin") },
		"null origin":      func(request *http.Request) { request.Header.Set("Origin", "null") },
		"foreign origin":   func(request *http.Request) { request.Header.Set("Origin", "https://foreign.example") },
		"duplicate origin": func(request *http.Request) { request.Header.Add("Origin", server.config.PublicOrigin.String()) },
		"cross-site fetch": func(request *http.Request) { request.Header.Set("Sec-Fetch-Site", "cross-site") },
		"duplicate fetch metadata": func(request *http.Request) {
			request.Header.Add("Sec-Fetch-Site", "same-origin")
		},
		"profile version": func(request *http.Request) { request.Header.Set("Sec-WebSocket-Protocol", netplay.ProtocolVersion) },
		"duplicate protocol": func(request *http.Request) {
			request.Header.Add("Sec-WebSocket-Protocol", netplay.WebSocketSubprotocol)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			request := valid()
			mutate(request)
			testassert.False(t, server.validNetplaySocketRequest(request), "invalid WebSocket request was accepted")
		})
	}
}
