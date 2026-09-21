package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"

	"retrom/internal/config"
	"retrom/internal/netplay"
)

func TestNetplayHandshakeAcceptsPublicOriginBehindInternalProxyHost(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://retrom.example")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{config: config.Config{PublicOrigin: origin}}
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !server.validNetplaySocketRequest(request) {
			http.Error(writer, "NETPLAY_ORIGIN_REJECTED", http.StatusForbidden)
			return
		}
		connection, err := server.acceptNetplaySocket(writer, request)
		if err != nil {
			return
		}
		defer func() { _ = connection.CloseNow() }()
		_ = connection.Write(request.Context(), websocket.MessageText, []byte("ready"))
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, backend.URL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": {origin.String()}},
		Subprotocols: []string{netplay.WebSocketSubprotocol},
	})
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		t.Fatalf("public origin with internal proxy Host: response=%v error=%v", response, err)
	}
	defer func() { _ = connection.CloseNow() }()
	_, message, err := connection.Read(ctx)
	if err != nil || string(message) != "ready" {
		t.Fatalf("handshake data = %q, %v", message, err)
	}
}
