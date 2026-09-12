package accounts

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/config"
	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"
)

func TestUserDeletionLateFailureRollsBackSecurityAndAudit(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	initial := authenticatedTestAdmin(t, fixture)
	acceptFixtureInvitation(t, fixture, initial.Principal, "ADMIN", "otheradmin", "Other Admin")
	repository := accountpersistence.NewAdministration(fixture.database.SQL)
	err := repository.WithWrite(t.Context(), func(scope accountservice.AdministrationScope) error {
		before, found, err := scope.Read.Current(t.Context(), initial.User.UserID, fixture.now.UnixMilli())
		if err != nil {
			return err
		}
		if !found {
			t.Fatal("missing initial administrator")
		}
		if err := scope.Write.Delete(t.Context(), accountservice.AdministrationDeletion{Before: before, Security: accountservice.UserSecurity{Sessions: true, CreatedLinks: true, TargetLinks: true, Launches: true, Reason: "USER_DELETED"}, ClearTestDefault: true, Now: fixture.now.UnixMilli()}); err != nil {
			return err
		}
		if err := scope.Write.Audit(t.Context(), accountservice.AccountAudit{ID: "rollback-admin-audit", ActorID: initial.User.UserID, Action: "USER_DELETED", ResourceType: "USER", ResourceID: initial.User.UserID, Now: fixture.now.UnixMilli()}); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late deletion: %v", err)
	}
	if _, err := fixture.service.Authenticate(t.Context(), initial.CookieToken); err != nil {
		t.Fatalf("failed deletion revoked session: %v", err)
	}
	if _, err := fixture.service.Login(t.Context(), "test", "test"); err != nil {
		t.Fatalf("failed deletion removed credential: %v", err)
	}
	var active, audits int
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT test_default_password_active,(SELECT count(*) FROM audit_events WHERE id='rollback-admin-audit') FROM instance_state`).Scan(&active, &audits); err != nil {
		t.Fatal(err)
	}
	if active != 1 || audits != 0 {
		t.Fatalf("partial deletion: default=%d audits=%d", active, audits)
	}
}
