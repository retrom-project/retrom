package launch

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestAdapterLaunchHasNoLegacyProviderAuthority(t *testing.T) {
	architecture.AssertNoLegacyLaunchAuthority(t)
}
