package composition

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
)

func TestOfflineRecoveryLateFailureKeepsCredentialAndSession(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	repository := accountpersistence.NewRecovery(fixture.database.SQL)
	target, found, err := repository.ByUsername(t.Context(), "test")
	if err != nil || !found {
		t.Fatalf("recovery target: %v", err)
	}
	err = repository.CommitWrite(t.Context(), func(scope accountservice.RecoveryScope) error {
		if err := scope.Write.Reset(t.Context(), accountservice.RecoveryPlan{Target: target, PasswordHash: "replacement-hash", AuditID: "rollback-audit", BeforeJSON: `{}`, AfterJSON: `{}`, ClearTestDefault: true, Now: fixture.now.UnixMilli()}); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late recovery failure: %v", err)
	}
	if _, err := fixture.service.Authenticate(t.Context(), session.CookieToken); err != nil {
		t.Fatalf("failed recovery revoked original session: %v", err)
	}
	if _, err := fixture.service.Login(t.Context(), "test", "test"); err != nil {
		t.Fatalf("failed recovery changed credential: %v", err)
	}
	var active, audits int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT test_default_password_active,(SELECT count(*) FROM audit_events WHERE id='rollback-audit') FROM instance_state WHERE id=1`).Scan(&active, &audits); err != nil {
		t.Fatal(err)
	}
	if active != 1 || audits != 0 {
		t.Fatalf("partial recovery: default=%d audit=%d", active, audits)
	}
}
