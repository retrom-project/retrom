//go:build integration

package httpapi

import (
	"testing"

	"retrom/internal/launch"
)

func assertSavedCoreChoice(t *testing.T, server *Server, gameID, saveID string, coreID *string, expected string) {
	t.Helper()
	saved, err := server.launcher.Create(t.Context(), "local", launch.CreateRequest{
		GameID: gameID, CoreID: coreID, SaveStateID: &saveID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
	})
	if err != nil || saved.LaunchID == "" || saved.Status == "VALIDATION_PENDING" {
		t.Fatalf("saved launch = %+v, error=%v", saved, err)
	}
	var core string
	if err := server.database.QueryRowContext(t.Context(), `SELECT core_id FROM launch_sessions WHERE id=?`, saved.LaunchID).Scan(&core); err != nil || core != expected {
		t.Fatalf("saved launch core = %q, want %q, error=%v", core, expected, err)
	}
}
