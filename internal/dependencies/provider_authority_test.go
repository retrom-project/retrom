package dependencies

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDependencyProductionCodeDoesNotOwnRuntimeProviderContract(t *testing.T) {
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"selected_core_" + "artifacts",
		"adapter_" + "abi",
		"runtime_" + "family",
		"route_" + "key",
		"RetromRuntime" + "File",
		"RPGMaker" + "Version",
	}
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, token := range forbidden {
			if strings.Contains(string(contents), token) {
				t.Errorf("%s retains Provider-owned token %q", path, token)
			}
		}
	}
}
