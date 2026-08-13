package netplay

import (
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/dependencies"
)

func TestRegistryRejectsDependencyDriftAndProducesStableCanonicalProfile(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "netplay", "v1", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	set := fixtureDependencySet()
	registry, err := parseRegistry(contents, set)
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := registry.Profile("fbneo-423-ldrun-v1")
	if !ok {
		t.Fatal("expected profile missing")
	}
	canonicalA, digestA, err := registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: profile, CoreArtifactID: "01980000-0000-7000-8000-000000000001",
		GameVariantRevisionID:  "01980000-0000-7000-8000-000000000002",
		DependencySnapshotJSON: `{"schemaVersion":1}`, DefaultCoreOptions: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalB, digestB, err := registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: profile, CoreArtifactID: "01980000-0000-7000-8000-000000000001",
		GameVariantRevisionID:  "01980000-0000-7000-8000-000000000002",
		DependencySnapshotJSON: `{"schemaVersion":1}`, DefaultCoreOptions: map[string]string{},
	})
	if err != nil || string(canonicalA) != string(canonicalB) || digestA != digestB || !validDigest(digestA) {
		t.Fatalf("canonical profile drift: %s/%s error=%v", digestA, digestB, err)
	}
	set.Versions["4.2.3"].Manifest.EmulatorJS.SelectedCores[0].SHA256 = "wrong"
	if _, err := parseRegistry(contents, set); err == nil {
		t.Fatal("dependency artifact drift accepted")
	}
}

func fixtureDependencySet() *dependencies.Set {
	version := &dependencies.Version{}
	version.Manifest.EmulatorJS.Version = "4.2.3"
	version.Manifest.EmulatorJS.PlayerAdapter.ID = PlayerAdapterID
	version.Manifest.EmulatorJS.SelectedCores = []dependencies.SelectedCore{
		{CoreID: "fceumm", SHA256: "8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "fbneo", SHA256: "315a25e0bcd61d58ee0d9e8b1dbf3740b9e0ca4b7d0726f848ce1068de73437c", SupportedContentKinds: []string{"SINGLE_FILE"}},
	}
	return &dependencies.Set{Versions: map[string]*dependencies.Version{"4.2.3": version}}
}
