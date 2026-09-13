package dependencies

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestDependencyProductionCodeDoesNotOwnRuntimeProviderContract(t *testing.T) {
	architecture.AssertNoProviderAuthority(t)
}
