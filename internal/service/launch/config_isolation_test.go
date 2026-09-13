package launch

import (
	"errors"
	"testing"

	"retrom/internal/runtimebundle"
)

func TestConfigIsolationRechecksRevocationAndExpiry(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		issuer, repository, builder := configTestFixture()
		ticket := IsolationTicket{Origin: "http://launch.localhost:3000", Ticket: "ticket", Hash: [32]byte{1}}
		source := &repository.snapshot.Authority.Source
		source.Delivery = "ISOLATED_WEB_PROJECT"
		repository.current.Source = *source
		grant := IsolationGrant{Origin: ticket.Origin, TicketHash: ticket.Hash[:], ExpiresAtMS: 1500}
		repository.snapshot.Authority.Isolation = []IsolationGrant{grant}
		repository.current.Isolation = []IsolationGrant{grant}
		issuer.environment.SignIsolation = func(string) (IsolationTicket, error) { return ticket, nil }
		builder.afterBuild = func() {
			if expired {
				repository.current.Isolation[0].ExpiresAtMS = 1000
			} else {
				repository.current.Isolation = nil
			}
		}
		configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
		assertConfigRejected(t, configuration, err, ErrBlocked)
		if repository.activations != 0 {
			t.Fatal("lost isolation grant activated")
		}
	}
}

func TestConfigIsolationAcceptsConsumedTicketWithLiveCapability(t *testing.T) {
	t.Parallel()
	ticket := IsolationTicket{Origin: "http://launch.localhost:3000", Ticket: "ticket", Hash: [32]byte{1}}
	grants := []IsolationGrant{{Origin: ticket.Origin, ExpiresAtMS: 1500}}
	if !validIsolationGrant(grants, ticket, 1000) {
		t.Fatal("live capability failed to replace consumed bootstrap")
	}
	for _, grant := range []IsolationGrant{
		{Origin: "http://other.localhost:3000", ExpiresAtMS: 1500},
		{Origin: ticket.Origin, ExpiresAtMS: 1000},
		{Origin: ticket.Origin, ExpiresAtMS: 1500, TicketHash: []byte("wrong")},
	} {
		if validIsolationGrant([]IsolationGrant{grant}, ticket, 1000) {
			t.Fatal("invalid origin/hash/expiry accepted")
		}
	}
}

func TestConfigRTPRemainsExplicitlyAbsent(t *testing.T) {
	t.Parallel()
	snapshot := ConfigSnapshot{Authority: ConfigAuthority{Source: ConfigSource{ContentKind: "RPG_MAKER_PROJECT"}}}
	target := runtimebundle.Target{Inputs: []runtimebundle.Input{{Role: "rtp", Kind: "FILE_TREE", Optional: true}}}
	resources, err := providerResources(snapshot, target, IsolationTicket{})
	if err != nil || len(resources) != 0 {
		t.Fatalf("historical RTP was mounted: count=%d error=%v", len(resources), err)
	}
	target.Inputs[0].Optional = false
	if _, err := providerResources(snapshot, target, IsolationTicket{}); !errors.Is(err, ErrCredential) {
		t.Fatalf("missing required RTP accepted: %v", err)
	}
}
