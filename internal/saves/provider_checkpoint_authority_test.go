package saves

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveProductionCodeTreatsProviderCheckpointsAsOpaque(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"core_artifact", "runtime_family", "route_key", "adapter_abi", "save_abi",
		"payload_kind", "native_profile", "resume_slot", "rpgmaker/checkpoint",
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		contents, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, token := range forbidden {
			if strings.Contains(string(contents), token) {
				t.Errorf("%s still contains legacy checkpoint authority %q", name, token)
			}
		}
	}
}
