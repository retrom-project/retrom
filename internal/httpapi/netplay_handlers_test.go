package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/netplay"
)

func TestNetplayFeatureFlagHidesRoutesAndAuthProjection(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	t.Cleanup(server.Close)
	handler := server.Handler()
	auth := accountHTTPLogin(t, handler)

	contextRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/context", nil)
	contextRequest.AddCookie(auth.cookie)
	contextResponse := httptest.NewRecorder()
	handler.ServeHTTP(contextResponse, contextRequest)
	if contextResponse.Code != http.StatusOK || !strings.Contains(contextResponse.Body.String(), `"netplayEnabled":false`) {
		t.Fatalf("disabled auth context = %d %s", contextResponse.Code, contextResponse.Body.String())
	}

	routeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/netplay/rooms", nil)
	routeRequest.AddCookie(auth.cookie)
	routeResponse := httptest.NewRecorder()
	handler.ServeHTTP(routeResponse, routeRequest)
	if routeResponse.Code != http.StatusNotFound {
		t.Fatalf("disabled netplay route = %d %s", routeResponse.Code, routeResponse.Body.String())
	}
}

func TestNetplayRoomCreateIsAuthenticatedIdempotentAndVersioned(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	server.config.NetplayEnabled = true
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := netplay.LoadRegistry(filepath.Join(repositoryRoot, "data"), server.dependencies)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := netplay.LoadOrCreateCredentials(server.config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := netplay.NewService(server.database, registry, credentials, netplay.Options{
		MaxActiveRooms: 16, DraftIdle: 15 * time.Minute, WaitingIdle: 30 * time.Minute, ReconnectLease: 10 * time.Second,
	}, time.Now)
	server.WithNetplay(service)
	t.Cleanup(server.Close)
	handler := server.Handler()

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/netplay/rooms", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous room list = %d %s", unauthenticated.Code, unauthenticated.Body.String())
	}

	auth := accountHTTPLogin(t, handler)
	key := uuid.NewString()
	sendCreate := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/netplay/rooms", strings.NewReader(`{}`))
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
	if created.Code != http.StatusCreated || replay.Code != http.StatusCreated || replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" || created.Body.String() != replay.Body.String() {
		t.Fatalf("create/replay = %d %s / %d %s", created.Code, created.Body.String(), replay.Code, replay.Body.String())
	}
	var room netplay.Room
	if err := json.Unmarshal(created.Body.Bytes(), &room); err != nil || room.State != netplay.RoomStateDraft || room.Version != 1 || room.SelfMemberID == nil || !room.Permissions.Host {
		t.Fatalf("created room = %#v, %v", room, err)
	}
	if created.Header().Get("ETag") != `"v1"` || created.Header().Get("Location") != "/api/v1/netplay/rooms/"+room.RoomID {
		t.Fatalf("create headers = %#v", created.Header())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/netplay/rooms/"+room.RoomID, nil)
	getRequest.AddCookie(auth.cookie)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || getResponse.Header().Get("ETag") != `"v1"` {
		t.Fatalf("get room = %d %s", getResponse.Code, getResponse.Body.String())
	}
}

func TestAcceptanceNP010NetplaySocketRejectsEveryNonExactOriginAndProtocol(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	t.Cleanup(server.Close)
	valid := func() *http.Request {
		request := httptest.NewRequest(http.MethodGet, "/runtime/netplay/rooms/room/socket", nil)
		request.Header.Set("Origin", server.config.PublicOrigin.String())
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("Sec-WebSocket-Protocol", netplay.WebSocketSubprotocol)
		return request
	}
	if !server.validNetplaySocketRequest(valid()) {
		t.Fatal("contract WebSocket subprotocol was rejected")
	}
	withoutFetchMetadata := valid()
	withoutFetchMetadata.Header.Del("Sec-Fetch-Site")
	if !server.validNetplaySocketRequest(withoutFetchMetadata) {
		t.Fatal("browser WebSocket request without Fetch Metadata was rejected")
	}
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
			if server.validNetplaySocketRequest(request) {
				t.Fatal("invalid WebSocket request was accepted")
			}
		})
	}
}
