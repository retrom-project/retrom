package launch

import (
	"math"
	"testing"
)

func TestConfigRevisionAllowsOnlyForwardActiveProgress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, beforeState, afterState string
		beforeVersion, afterVersion   int64
		allowed                       bool
		activations                   int
	}{
		{"created unchanged", "CREATED", "CREATED", 2, 2, true, 1},
		{"created changed", "CREATED", "CREATED", 2, 3, false, 0},
		{"concurrent activation", "CREATED", "ACTIVE", 2, 3, true, 0},
		{"concurrent start", "CREATED", "ACTIVE", 2, 4, true, 0},
		{"concurrent heartbeat", "CREATED", "ACTIVE", 2, 5, true, 0},
		{"active without activation revision", "CREATED", "ACTIVE", 2, 2, false, 0},
		{"activation revision regressed", "CREATED", "ACTIVE", 2, 1, false, 0},
		{"active unchanged", "ACTIVE", "ACTIVE", 2, 2, true, 0},
		{"active progressed", "ACTIVE", "ACTIVE", 2, 4, true, 0},
		{"active version regressed", "ACTIVE", "ACTIVE", 2, 1, false, 0},
		{"active state regressed", "ACTIVE", "CREATED", 2, 3, false, 0},
		{"last activation revision", "CREATED", "ACTIVE", math.MaxInt64 - 1, math.MaxInt64, true, 0},
		{"no representable activation revision", "CREATED", "ACTIVE", math.MaxInt64, math.MaxInt64, false, 0},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			issuer, repository, _ := configTestFixture()
			repository.snapshot.Authority.Source.State = item.beforeState
			repository.snapshot.Authority.Source.Version = item.beforeVersion
			repository.current.Source.State = item.afterState
			repository.current.Source.Version = item.afterVersion
			configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
			if item.allowed {
				if err != nil {
					t.Fatalf("legitimate activation/play revision rejected: %v", err)
				}
				if _, err := configuration.MarshalJSON(); err != nil {
					t.Fatal(err)
				}
			} else {
				assertConfigRejected(t, configuration, err, ErrCredential)
			}
			if repository.activations != item.activations {
				t.Fatalf("activation count=%d want=%d", repository.activations, item.activations)
			}
		})
	}
}
