package composition

import (
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
)

func TestOfflineRecoveryVersionMismatchKeepsCredentialAndSession(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	session := authenticatedTestAdmin(t, fixture)
	repository := accountpersistence.NewRecovery(fixture.database.SQL)
	target, found, err := repository.ByUsername(t.Context(), "test")
	if err != nil || !found {
		t.Fatalf("recovery target: %v", err)
	}
	err = repository.CommitRecovery(t.Context(), accountservice.RecoveryCommand{
		UserID:       target.UserID,
		Version:      target.Version + 999,
		PasswordHash: "replacement-hash",
		AuditID:      "bad-recovery-audit",
		NowMS:        fixture.now.UnixMilli(),
	})
	if err == nil {
		t.Fatal("mismatched version should fail")
	}
	if _, err := fixture.service.Authenticate(t.Context(), session.CookieToken); err != nil {
		t.Fatalf("failed recovery revoked original session: %v", err)
	}
	if _, err := fixture.service.Login(t.Context(), "test", "test"); err != nil {
		t.Fatalf("failed recovery changed credential: %v", err)
	}
	var active int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT test_default_password_active FROM instance_state WHERE id=1`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("partial recovery: default=%d", active)
	}
}
