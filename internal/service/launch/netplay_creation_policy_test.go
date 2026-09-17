package launch

import (
	"errors"
	model "retrom/internal/model/launch"
	"testing"
)

func TestNetplayCreatorRejectsFinalAuthorityAndFrozenInputChanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*model.NetplayCreationSnapshot)
	}{
		{"terminal session", func(s *model.NetplayCreationSnapshot) { s.Authority.SessionState = "FAILED" }},
		{"replaced room session", func(s *model.NetplayCreationSnapshot) { s.Authority.CurrentSessionID = "another" }},
		{"left participant", func(s *model.NetplayCreationSnapshot) { s.Authority.ParticipantState = "LEFT" }},
		{"changed participant version", func(s *model.NetplayCreationSnapshot) { s.Authority.ParticipantVersion++ }},
		{"changed target", func(s *model.NetplayCreationSnapshot) { s.Authority.TargetID = "another" }},
		{"changed owner", func(s *model.NetplayCreationSnapshot) { s.Authority.ProfileID = "another" }},
		{"changed content", func(s *model.NetplayCreationSnapshot) { s.Product.GameFiles[0].BlobID = "replacement" }},
		{"changed snapshot", func(s *model.NetplayCreationSnapshot) { s.Product.Source.DependencySnapshot = "{}" }},
		{"blocked variant", func(s *model.NetplayCreationSnapshot) { s.Product.Source.VariantStatus = "BLOCKED" }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			service, repository, request := netplayCreatorFixture(t)
			item.change(&repository.current)
			result, err := service.CreateNetplay(t.Context(), request)
			if !errors.Is(err, model.ErrBlocked) || result.LaunchID != "" || len(repository.writes) != 0 {
				t.Fatalf("changed authority returned launch=%q error=%v", result.LaunchID, err)
			}
		})
	}
}

func TestNetplayCreatorRejectsStaleExistingCredential(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"generation", "hash", "launch owner", "launch credential", "expired", "left"} {
		t.Run(kind, func(t *testing.T) {
			service, repository, request := netplayCreatorFixture(t)
			existingNetplaySnapshot(&repository.before, request)
			switch kind {
			case "generation":
				repository.before.Authority.Generation++
			case "hash":
				repository.before.Authority.CredentialHash[0] = 1
			case "launch owner":
				repository.before.Existing.ProfileID = "another"
			case "launch credential":
				repository.before.Existing.CredentialHash[0] = 1
			case "expired":
				repository.before.Existing.HardEnd = 1000
			case "left":
				repository.before.Authority.ParticipantState = "LEFT"
			}
			repository.current = cloneNetplaySnapshot(t, repository.before)
			result, err := service.CreateNetplay(t.Context(), request)
			if !errors.Is(err, model.ErrBlocked) || result.LaunchID != "" || len(repository.writes) != 0 {
				t.Fatalf("stale existing launch returned: %v", err)
			}
		})
	}
}

func TestNetplayCreatorRejectsUnavailableProviderAndThreadCapabilities(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"netplay port", "bundle", "secure context", "isolation", "shared memory"} {
		t.Run(kind, func(t *testing.T) {
			service, repository, request := netplayCreatorFixture(t)
			provider := netplayCreationProvider{digest: request.BundleSHA256}
			provider.target.Capabilities.NetplayPort = true
			provider.target.Capabilities.RequiresThreads = true
			request.ClientCapabilities = model.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
			switch kind {
			case "netplay port":
				provider.target.Capabilities.NetplayPort = false
			case "bundle":
				provider.digest = "different"
			case "secure context":
				request.ClientCapabilities.SecureContext = false
			case "isolation":
				request.ClientCapabilities.CrossOriginIsolated = false
			case "shared memory":
				request.ClientCapabilities.SharedArrayBuffer = false
			}
			service.provider = provider
			result, err := service.CreateNetplay(t.Context(), request)
			if !errors.Is(err, model.ErrBlocked) || result.LaunchID != "" || len(repository.writes) != 0 {
				t.Fatalf("provider constraint escaped: %v", err)
			}
		})
	}
}
