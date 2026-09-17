package composition

import (
	"testing"

	"retrom/internal/bootstrap/config"
	accountservice "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
)

func TestUserDeletionAtomicityPreservesSessionAndCredential(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	initial := authenticatedTestAdmin(t, fixture)
	acceptFixtureInvitation(t, fixture, initial.Principal, "ADMIN", "otheradmin", "Other Admin")
	repository := accountpersistence.NewAdministration(fixture.database.SQL)
	_, err := repository.CommitDeleteUser(t.Context(), accountservice.DeleteUserCommand{
		TargetID:     initial.User.UserID,
		ActorID:      initial.User.UserID,
		Confirmation: "wrong-confirmation",
		Operation: accountservice.AccountOperation{
			PrincipalID: initial.User.UserID,
			Now:         fixture.now.UnixMilli(),
		},
	})
	if err == nil {
		t.Fatal("deletion with wrong confirmation should fail")
	}
	if _, err := fixture.service.Authenticate(t.Context(), initial.CookieToken); err != nil {
		t.Fatalf("failed deletion revoked session: %v", err)
	}
	if _, err := fixture.service.Login(t.Context(), "test", "test"); err != nil {
		t.Fatalf("failed deletion removed credential: %v", err)
	}
}
