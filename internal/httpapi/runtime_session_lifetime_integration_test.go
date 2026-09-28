//go:build integration

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/launch"
	runtimepersistence "retrom/internal/persistence/runtimesession"
	"retrom/internal/service/runtimesession"

	"github.com/google/uuid"
)

func TestRuntimeRenewalKeepsActiveLaunchAndPayloadAliveAcrossDays(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	now := time.Now()
	server.now = func() time.Time { return now }
	server.playDeps.RuntimeSessions = runtimesession.New(runtimepersistence.New(server.database), runtimesession.Environment{
		Now: server.now, Sign: server.contentDeps.Credentials.RuntimeSession,
		NewID: func() (string, error) { id, err := uuid.NewV7(); return id.String(), err },
	})
	created, err := server.playDeps.Launcher.Create(t.Context(), "local", launch.CreateRequest{
		GameID: gameID, ReturnTo: "/games/" + gameID, ClientCapabilities: launch.Capabilities{
			SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.playDeps.Launcher.Config(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}
	cookie := testRuntimeCookie(t, server)
	handler := server.Handler()
	for range 5 {
		now = now.Add(12*time.Hour + time.Millisecond)
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/runtime/launches/"+created.LaunchID+"/renew", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("renew status=%d body=%s", response.Code, response.Body)
		}
		renewed := response.Result().Cookies()
		if len(renewed) != 1 || renewed[0].Value != cookie.Value {
			t.Fatal("renewal changed shared token")
		}
		var expiry, retirement int64
		err = dbapi.QueryRowContext(t.Context(), server.database, `SELECT l.hard_expires_at_ms,r.due_at_ms
 FROM launch_sessions l JOIN launch_payload_retirements r ON r.launch_session_id=l.id WHERE l.id=?`, created.LaunchID).Scan(&expiry, &retirement)
		if err != nil {
			t.Fatal(err)
		}
		if expiry != now.Add(24*time.Hour).UnixMilli() || retirement != expiry {
			t.Fatal("active launch and its payload retirement must follow runtime renewal")
		}
	}
}
