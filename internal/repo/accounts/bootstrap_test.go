package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/service/accounts"
	"retrom/internal/testkit/testsupport"
)

func TestBootstrapLateFailureRollsBackIdentitySessionStateAndAudit(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	plan := accounts.BootstrapPlan{UserID: "initial-user", ProfileID: "initial-profile", Username: "admin", DisplayName: "Owner", PasswordHash: "initial-hash", Kind: "RELEASE_SETUP", ActorLabel: "release-setup", AuditID: "initial-audit", Now: 100}
	plan.Session = accounts.SessionRecord{ID: "initial-session", UserID: plan.UserID, SessionVersion: 1, CreatedAt: 100, LastSeen: 100, IdleExpiry: 200, AbsoluteExpiry: 300}
	err = NewInitialization(database.SQL).CommitWrite(t.Context(), func(scope accounts.InitializationScope) error {
		if err := scope.Write.Bootstrap(t.Context(), plan); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late bootstrap failure: %v", err)
	}
	var state string
	var users, profiles, credentials, sessions, audit int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT state,(SELECT count(*) FROM users),(SELECT count(*) FROM profiles),(SELECT count(*) FROM user_credentials),(SELECT count(*) FROM auth_sessions),(SELECT count(*) FROM audit_events) FROM instance_state WHERE id=1`).Scan(&state, &users, &profiles, &credentials, &sessions, &audit); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" || users != 0 || profiles != 0 || credentials != 0 || sessions != 0 || audit != 0 {
		t.Fatalf("partial bootstrap: %s / %d %d %d %d %d", state, users, profiles, credentials, sessions, audit)
	}
}
