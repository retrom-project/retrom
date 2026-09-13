package storageanalysis

import (
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestStorageAnalysisUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	architecture.AssertBusinessImports(t)
}
