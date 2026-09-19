package isolation

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	isolationmodel "retrom/internal/model/isolation"
)

func TestFailedCapabilityIssueDoesNotConsumeTicket(t *testing.T) {
	t.Parallel()
	fixture := newIsolationFixture(t)
	raw, err := base64.RawURLEncoding.DecodeString(fixture.ticket)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	query := isolationmodel.TicketQuery{LaunchID: fixture.launchID, Origin: fixture.origin, Digest: &digest}
	err = fixture.repository.WithWrite(t.Context(), func(records isolationmodel.Tickets) error {
		if err := records.Consume(t.Context(), query, *fixture.nowMS); err != nil {
			return err
		}
		return records.Issue(t.Context(), isolationmodel.CapabilityWrite{
			Digest: digest, IssuedAtMS: *fixture.nowMS,
			Access: isolationmodel.Access{LaunchID: fixture.launchID, Origin: fixture.origin, Profile: "wrong-owner", Expires: *fixture.nowMS + 1000},
		})
	})
	if err == nil {
		t.Fatal("capability with a mismatched owner was issued")
	}
	if _, err := fixture.service.InspectBootstrap(t.Context(), fixture.launchID, fixture.origin); err != nil {
		t.Fatalf("failed issue consumed ticket: %v", err)
	}
	if _, _, err := fixture.service.ConsumeTicket(t.Context(), fixture.launchID, fixture.origin, fixture.ticket); err != nil {
		t.Fatalf("valid retry failed: %v", err)
	}
}
