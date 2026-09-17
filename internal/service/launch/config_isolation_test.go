package launch

import (
	"errors"
	"testing"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/runtime/runtimebundle"
)

func TestConfigIsolationRechecksRevocationAndExpiry(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		issuer, repository, builder := configTestFixture()
		ticket := model.IsolationTicket{Origin: "http://launch.localhost:3000", Ticket: "ticket", Hash: [32]byte{1}}
		source := &repository.snapshot.Authority.Source
		source.Delivery = "ISOLATED_WEB_PROJECT"
		repository.current.Source = *source
		grant := model.IsolationGrant{Origin: ticket.Origin, TicketHash: ticket.Hash[:], ExpiresAtMS: 1500}
		repository.snapshot.Authority.Isolation = []model.IsolationGrant{grant}
		repository.current.Isolation = []model.IsolationGrant{grant}
		issuer.environment.SignIsolation = func(string) (model.IsolationTicket, error) { return ticket, nil }
		builder.afterBuild = func() {
			if expired {
				repository.current.Isolation[0].ExpiresAtMS = 1000
			} else {
				repository.current.Isolation = nil
			}
		}
		configuration, err := issuer.Issue(t.Context(), model.SessionRef{ID: "launch"}, "valid")
		assertConfigRejected(t, configuration, err, model.ErrBlocked)
		if repository.activations != 0 {
			t.Fatal("lost isolation grant activated")
		}
	}
}

func TestConfigIsolationAcceptsConsumedTicketWithLiveCapability(t *testing.T) {
	t.Parallel()
	ticket := model.IsolationTicket{Origin: "http://launch.localhost:3000", Ticket: "ticket", Hash: [32]byte{1}}
	grants := []model.IsolationGrant{{Origin: ticket.Origin, ExpiresAtMS: 1500}}
	if !validIsolationGrant(grants, ticket, 1000) {
		t.Fatal("live capability failed to replace consumed bootstrap")
	}
	for _, grant := range []model.IsolationGrant{
		{Origin: "http://other.localhost:3000", ExpiresAtMS: 1500},
		{Origin: ticket.Origin, ExpiresAtMS: 1000},
		{Origin: ticket.Origin, ExpiresAtMS: 1500, TicketHash: []byte("wrong")},
	} {
		if validIsolationGrant([]model.IsolationGrant{grant}, ticket, 1000) {
			t.Fatal("invalid origin/hash/expiry accepted")
		}
	}
}

func TestConfigRTPRemainsExplicitlyAbsent(t *testing.T) {
	t.Parallel()
	snapshot := model.ConfigSnapshot{Authority: model.ConfigAuthority{Source: model.ConfigSource{ContentKind: "RPG_MAKER_PROJECT"}}}
	target := runtimebundle.Target{Inputs: []runtimebundle.Input{{Role: "rtp", Kind: "FILE_TREE", Optional: true}}}
	resources, err := providerResources(snapshot, target, model.IsolationTicket{})
	if err != nil || len(resources) != 0 {
		t.Fatalf("historical RTP was mounted: count=%d error=%v", len(resources), err)
	}
	target.Inputs[0].Optional = false
	if _, err := providerResources(snapshot, target, model.IsolationTicket{}); !errors.Is(err, model.ErrCredential) {
		t.Fatalf("missing required RTP accepted: %v", err)
	}
}
