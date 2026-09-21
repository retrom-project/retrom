package architecture

import "testing"

func TestStorageAnalysisUsesBusinessPorts(t *testing.T) {
	t.Parallel()
	assertBusinessImports(t, "../service/storageanalysis")
}
