package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPIUsesOnlyTheProviderLaunchEnvelopeAndOpaqueCheckpointContract(t *testing.T) {
	root := filepath.Join("..", "..", "api")
	paths := []string{
		filepath.Join(root, "components", "common.yaml"),
		filepath.Join(root, "domains", "runtime.yaml"),
		filepath.Join(root, "domains", "reviews.yaml"),
		filepath.Join(root, "domains", "netplay.yaml"),
	}
	var contents strings.Builder
	for _, path := range paths {
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		contents.Write(value)
	}
	source := contents.String()
	if !strings.Contains(source, "$ref: ../runtime-provider/v1/launch-envelope.schema.json") {
		t.Fatal("runtime OpenAPI does not reference the authoritative Launch Envelope")
	}
	for _, forbidden := range []string{
		"runtimeFamily", "playerAdapterId", "coreArtifactId", "adapterAbi", "saveAbi",
		"payloadKind", "nativeProfile", "resumeSlot", "readableSaveAbis",
		"emulatorjsVersion",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy runtime field %q remains in OpenAPI", forbidden)
		}
	}
}
