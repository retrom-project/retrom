package netplayprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type registryGolden struct {
	RawManifestSHA256 string
	ProtocolJSON      string
	ManifestJSON      string
	Profiles          []canonicalGolden
}
type canonicalGolden struct {
	ProfileID       string
	CanonicalJSON   string
	CanonicalSHA256 string
}

func compatibilityRegistry(t *testing.T) ([]byte, *Registry, registryGolden) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "data", "netplay", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ParseRegistry(contents, fixtureBindings())
	if err != nil {
		t.Fatal(err)
	}
	rawGolden, err := os.ReadFile(filepath.Join("testdata", "registry-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden registryGolden
	if err := json.Unmarshal(rawGolden, &golden); err != nil {
		t.Fatal(err)
	}
	return contents, registry, golden
}
