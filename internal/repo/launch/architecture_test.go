package launch

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestPersistenceLaunchDoesNotOwnLegacyProviderAuthority(t *testing.T) {
	t.Parallel()
	architecture.AssertNoLegacyLaunchAuthority(t)
}
