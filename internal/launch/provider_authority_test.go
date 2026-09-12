package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionLaunchHasOneProviderTargetEnvelopePath(t *testing.T) {
	var source strings.Builder
	for _, directory := range []string{".", "../service/launch", "../persistence/launch"} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			source.Write(contents)
		}
	}
	for _, forbidden := range []string{
		"runtime_family", "runtimeFamily", "route_key", "routeKey", "core_artifacts",
		"core_artifact_id", "CoreArtifactID", "adapterAbi", "saveAbi", "payloadKind",
		"nativeProfile", "resumeSlot",
	} {
		if strings.Contains(source.String(), forbidden) {
			t.Fatalf("legacy launch authority %q remains in production source", forbidden)
		}
	}
	if count := strings.Count(source.String(), "runtimeBuilder.Build("); count != 1 {
		t.Fatalf("runtimeBuilder.Build call count = %d, want one generic Envelope path", count)
	}
}
