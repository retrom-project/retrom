package runtimeprovider

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestRuntimeProviderActivationUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
