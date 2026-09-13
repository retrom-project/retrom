package launch

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestServiceLaunchDoesNotOwnLegacyProviderAuthority(t *testing.T) {
	t.Parallel()
	architecture.AssertNoLegacyLaunchAuthority(t)
}

func TestServiceLaunchBuildsOneProviderTargetEnvelope(t *testing.T) {
	t.Parallel()
	architecture.AssertExactlyOneRuntimeBuilderUse(t)
}
