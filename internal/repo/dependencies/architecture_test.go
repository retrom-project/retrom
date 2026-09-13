package dependencies

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestDependencyPersistenceDoesNotOwnRuntimeProviderContract(t *testing.T) {
	t.Parallel()
	architecture.AssertNoProviderAuthority(t)
}
