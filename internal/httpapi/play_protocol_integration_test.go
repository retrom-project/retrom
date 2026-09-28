//go:build integration

package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/launch"
)

func TestPreviewFinishRequiresNoBodyAndRetainsIdempotency(t *testing.T) {
	server, itemID := newCheckpointReviewHTTPFixture(t)
	preview, cookie := createCheckpointPreviewHTTP(t, server, itemID, nil)
	progress := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "POST", "progress", strings.NewReader(`{"activeDurationMs":1000}`), "application/json")
	if progress.Code != http.StatusUnauthorized {
		t.Fatalf("preview progress status=%d", progress.Code)
	}
	for _, endpoint := range []string{"start", "heartbeat"} {
		response := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "POST", endpoint, strings.NewReader(`{}`), "application/json")
		if response.Code != http.StatusNotFound {
			t.Fatalf("removed endpoint %s status=%d", endpoint, response.Code)
		}
	}
	invalid := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "POST", "finish", strings.NewReader(`{"clientSequence":0,"clientObservedAtMs":1,"previousInterval":null}`), "application/json")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("event body accepted: %d", invalid.Code)
	}
	denied := requestReviewCheckpointHTTP(t, server, preview.PreviewID, nil, "POST", "finish", nil, "")
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized finish: %d", denied.Code)
	}
	for range 2 {
		response := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "POST", "finish", nil, "")
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("preview finish=%d body size=%d", response.Code, response.Body.Len())
		}
	}
	var plays int
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT count(*) FROM play_sessions`).Scan(&plays); err != nil || plays != 0 {
		t.Fatalf("preview created %d stats: %v", plays, err)
	}
}

func TestProductProgressKeepsAccessAndRejectsPreviewFinish(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	created, err := server.playDeps.Launcher.Create(t.Context(), "local", launch.CreateRequest{GameID: gameID, ReturnTo: "/library", ClientCapabilities: launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "retrom_launch_" + created.LaunchID, Value: created.Capability}
	configuration := requestReviewCheckpointHTTP(t, server, created.LaunchID, cookie, "GET", "config", nil, "")
	if configuration.Code != http.StatusOK {
		t.Fatalf("config status=%d", configuration.Code)
	}
	var accepted int64
	for _, elapsed := range []int64{1000, 1000, 500, 2000} {
		accepted = max(accepted, elapsed)
		response := requestReviewCheckpointHTTP(t, server, created.LaunchID, cookie, "POST", "progress", strings.NewReader(fmt.Sprintf(`{"activeDurationMs":%d}`, elapsed)), "application/json")
		var result struct {
			ActiveDurationMS int64 `json:"activeDurationMs"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.ActiveDurationMS != accepted {
			t.Fatalf("sample %d: status=%d result=%#v", elapsed, response.Code, result)
		}
	}
	denied := requestReviewCheckpointHTTP(t, server, created.LaunchID, cookie, "POST", "finish", nil, "")
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("product finish status=%d", denied.Code)
	}
	if err := server.playDeps.Launcher.AuthorizeSave(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatalf("progress revoked save: %v", err)
	}
}
