package runtimecatalog

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestRuntimeCatalogUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
