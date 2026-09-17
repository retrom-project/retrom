package accounts

import (
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model/accounts"
	"retrom/internal/testkit/testsupport"
)

func TestBootstrapAtomicityEnsuresNoPartialState(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	plan := accounts.BootstrapPlan{
		UserID: "initial-user", ProfileID: "initial-profile", Username: "admin", DisplayName: "Owner",
		PasswordHash: "initial-hash", Kind: "RELEASE_SETUP", ActorLabel: "release-setup", AuditID: "initial-audit", Now: 100,
	}
	plan.Session = accounts.SessionRecord{ID: "initial-session", UserID: plan.UserID, SessionVersion: 1, CreatedAt: 100, LastSeen: 100, IdleExpiry: 200, AbsoluteExpiry: 300}
	err = NewInitialization(database.SQL).CommitBootstrap(t.Context(), accounts.BootstrapCommand{Plan: plan})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var state string
	var users int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT state,(SELECT count(*) FROM users) FROM instance_state WHERE id=1`).Scan(&state, &users); err != nil {
		t.Fatal(err)
	}
	if (state != "READY" && state != "COMPLETED") || users != 1 {
		t.Fatalf("bootstrap incomplete: state=%s users=%d", state, users)
	}
}
