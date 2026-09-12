package composition

import (
	"errors"
	"testing"

	"retrom/internal/config"
	accountservice "retrom/internal/service/accounts"
)

func TestAccountLinkInspectionPreservesDatabaseFailure(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	admin := authenticatedTestAdmin(t, fixture)
	link, _, err := fixture.service.CreateInvitation(t.Context(), admin.Principal, "USER", false, "invitation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(t.Context(), `DROP TABLE account_links`); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.InspectAccountLink(t.Context(), "INVITATION", link.CapabilityToken)
	if err == nil || errors.Is(err, accountservice.ErrAccountLinkUnavailable) {
		t.Fatalf("database failure classified as unavailable capability: %v", err)
	}
}

func TestAccountLinkDirectoryRejectsUnboundedLimit(t *testing.T) {
	fixture := newAccountFixture(t, config.ModeTest)
	authenticatedTestAdmin(t, fixture)
	_, err := fixture.service.ListAccountLinks(t.Context(), accountservice.LinkListFilter{Kind: "INVITATION", Limit: -1})
	if err == nil {
		t.Fatal("negative limit disabled the account-link directory bound")
	}
}
