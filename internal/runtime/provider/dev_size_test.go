package runtimeprovider

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDevProviderAllowsCorePayloadBeyondMetadataLimit(t *testing.T) {
	paths := writeInstallationFixture(t, t.TempDir())
	module := bytes.Repeat([]byte(" "), metadataLimit+1)
	publishDevModule(t, &paths, module)
	if _, err := LoadInstallation(paths.Paths); err != nil {
		t.Fatal(err)
	}
	// Formal provider metadata keeps its smaller limit.
	if _, err := readMetadata(filepath.Join(paths.DevRoot, "dev-provider.json")); err == nil {
		t.Fatal("oversized formal metadata accepted")
	}
}

func TestDevProviderRejectsOversizedPayload(t *testing.T) {
	paths := writeInstallationFixture(t, t.TempDir())
	publishDevModule(t, &paths, []byte("export{}"))
	path := filepath.Join(paths.DevRoot, "dev-provider.json")
	if err := os.Truncate(path, (128<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInstallation(paths.Paths); err == nil {
		t.Fatal("oversized dev payload accepted")
	}
}
