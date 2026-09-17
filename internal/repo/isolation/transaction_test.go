package isolation

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"retrom/internal/model/isolation"
)

func TestFailedCapabilityIssueDoesNotConsumeTicket(t *testing.T) {
	t.Parallel()
	fixture := newIsolationFixture(t)
	raw, err := base64.RawURLEncoding.DecodeString(fixture.ticket)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	_, err = fixture.repository.ConsumeAndIssue(t.Context(), isolation.ConsumeAndIssueCommand{
		Query:    isolation.TicketQuery{LaunchID: fixture.launchID, Origin: fixture.origin, Digest: &digest},
		LaunchID: fixture.launchID, Origin: "wrong-origin",
		Digest: digest, NowMS: *fixture.nowMS,
	})
	if err == nil {
		t.Fatal("capability with a mismatched origin was issued")
	}
	if _, err := fixture.service.InspectBootstrap(t.Context(), fixture.launchID, fixture.origin); err != nil {
		t.Fatalf("failed issue consumed ticket: %v", err)
	}
	if _, _, err := fixture.service.ConsumeTicket(t.Context(), fixture.launchID, fixture.origin, fixture.ticket); err != nil {
		t.Fatalf("valid retry failed: %v", err)
	}
}
