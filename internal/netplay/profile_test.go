package netplay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/dependencies"
	"retrom/internal/testassert"
)

func TestRegistryRejectsDependencyDriftAndProducesStableCanonicalProfile(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "netplay", "v1", "manifest.json"))
	testassert.False(t, err != nil, err)
	set := fixtureDependencySet()
	registry, err := parseRegistry(contents, set)
	testassert.False(t, err != nil, err)
	profile, ok := registry.Profile("fbneo-423-v1")
	testassert.True(t, ok, "expected profile missing")
	canonicalA, digestA, err := registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: profile, CoreArtifactID: "01980000-0000-7000-8000-000000000001",
		GameVariantRevisionID:  "01980000-0000-7000-8000-000000000002",
		DependencySnapshotJSON: `{"schemaVersion":1}`, DefaultCoreOptions: map[string]string{},
	})
	testassert.False(t, err != nil, err)
	canonicalB, digestB, err := registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: profile, CoreArtifactID: "01980000-0000-7000-8000-000000000001",
		GameVariantRevisionID:  "01980000-0000-7000-8000-000000000002",
		DependencySnapshotJSON: `{"schemaVersion":1}`, DefaultCoreOptions: map[string]string{},
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return string(canonicalA) != string(canonicalB) }, func() bool { return digestA != digestB }, func() bool { return !validDigest(digestA) }), "canonical profile drift: %s/%s error=%v", digestA, digestB, err)
	var canonicalProfile struct {
		MaxPredictionFrames int `json:"maxPredictionFrames"`
	}
	if err := json.Unmarshal(canonicalA, &canonicalProfile); err != nil || canonicalProfile.MaxPredictionFrames != 0 {
		t.Fatalf("FBNeo prediction limit not bound into canonical profile: %+v error=%v", canonicalProfile, err)
	}
	set.Versions["4.2.3"].Manifest.EmulatorJS.SelectedCores[0].SHA256 = "wrong"
	if _, err := parseRegistry(contents, set); err == nil {
		t.Fatal("dependency artifact drift accepted")
	}
}

func TestRegistryRejectsContentSpecificProfileFields(t *testing.T) {
	t.Parallel()
	contents := []byte(`{
  "schemaVersion":3,
  "protocol":{
    "version":"retrom-netplay-v1","playerAdapterId":"ejs-4.2.3-v2",
    "netplayAdapterId":"ejs-netplay-4.2.3-v1","controlCount":24,
    "checkpointEveryFrames":120,"maxPredictionFrames":8,"maxRollbackFrames":120,
    "canonicalHistoryFrames":600,"maxStateBytes":1048576,
    "allowedContentKinds":["SINGLE_FILE"]
  },
  "profiles":[{
    "id":"fceumm-423-v1","emulatorjsVersion":"4.2.3","coreId":"fceumm",
    "coreArtifactSha256":"8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493",
    "contentSha256":"29208764886f14de20fe82b32ab034130915f6392103874d202fcbbfb8a02ee4",
    "maxPlayers":2,"maxPredictionFrames":8
  }]
}`)
	if _, err := parseRegistry(contents, fixtureDependencySet()); err == nil {
		t.Fatal("content-specific profile field accepted")
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
