package dependencies

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestDependencyRulesUseBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}

func TestDependencyServiceDoesNotOwnRuntimeProviderContract(t *testing.T) {
	t.Parallel()
	architecture.AssertNoProviderAuthority(t)
}
