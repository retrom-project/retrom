package runtimeprovider

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestRuntimeProviderAdapterUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
