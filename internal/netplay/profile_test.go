package netplay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"retrom/internal/dependencies"
	"retrom/internal/testassert"
)

func TestRegistryRejectsDependencyDriftAndProducesStableCanonicalProfile(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", ManifestRelativePath))
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
		MaxPredictionFrames int      `json:"maxPredictionFrames"`
		PlatformIDs         []string `json:"platformIds"`
	}
	if err := json.Unmarshal(canonicalA, &canonicalProfile); err != nil ||
		canonicalProfile.MaxPredictionFrames != 0 || !slices.Equal(canonicalProfile.PlatformIDs, []string{"arcade"}) {
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
  "schemaVersion":4,
  "protocol":{
    "version":"retrom-netplay-v2","playerAdapterId":"ejs-4.2.3-v2",
    "netplayAdapterId":"ejs-netplay-4.2.3-v1","controlCount":24,
    "checkpointEveryFrames":120,"maxPredictionFrames":8,"maxRollbackFrames":120,
    "canonicalHistoryFrames":600,"maxStateBytes":1048576,
    "allowedContentKinds":["SINGLE_FILE"]
  },
  "profiles":[{
    "id":"fceumm-423-v1","emulatorjsVersion":"4.2.3","coreId":"fceumm","platformIds":["nes"],
    "coreArtifactSha256":"8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493",
    "contentSha256":"29208764886f14de20fe82b32ab034130915f6392103874d202fcbbfb8a02ee4",
    "maxPlayers":2,"maxPredictionFrames":8
  }]
}`)
	if _, err := parseRegistry(contents, fixtureDependencySet()); err == nil {
		t.Fatal("content-specific profile field accepted")
	}
}

func TestRegistryContainsOnlyTheEightArtifactBoundProfiles(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", ManifestRelativePath))
	testassert.False(t, err != nil, err)
	registry, err := parseRegistry(contents, fixtureDependencySet())
	testassert.False(t, err != nil, err)
	expected := map[string]struct {
		predictionFrames int
		platformID       string
	}{
		"fceumm-423-v1":            {predictionFrames: 8, platformID: "nes"},
		"fbneo-423-v1":             {predictionFrames: 0, platformID: "arcade"},
		"snes9x-423-v1":            {predictionFrames: 0, platformID: "snes"},
		"mame2003-423-override-v1": {predictionFrames: 0, platformID: "arcade"},
		"mame2003-plus-423-v1":     {predictionFrames: 0, platformID: "arcade"},
		"fbalpha2012-cps1-423-v1":  {predictionFrames: 0, platformID: "arcade"},
		"fbalpha2012-cps2-423-v1":  {predictionFrames: 0, platformID: "arcade"},
		"nestopia-423-v1":          {predictionFrames: 0, platformID: "nes"},
	}
	if len(registry.profiles) != len(expected) {
		t.Fatalf("profile count = %d, want %d", len(registry.profiles), len(expected))
	}
	for profileID, expectation := range expected {
		profile, ok := registry.Profile(profileID)
		if !ok || profile.MaxPredictionFrames != expectation.predictionFrames || profile.MaxPlayers != 2 ||
			!slices.Equal(profile.PlatformIDs, []string{expectation.platformID}) {
			t.Fatalf("profile %q = %+v, exists=%t", profileID, profile, ok)
		}
	}
}

func fixtureDependencySet() *dependencies.Set {
	version := &dependencies.Version{}
	version.Manifest.EmulatorJS.Version = "4.2.3"
	version.Manifest.EmulatorJS.PlayerAdapter.ID = PlayerAdapterID
	version.Manifest.EmulatorJS.SelectedCores = []dependencies.SelectedCore{
		{CoreID: "fceumm", SHA256: "8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "fbneo", SHA256: "315a25e0bcd61d58ee0d9e8b1dbf3740b9e0ca4b7d0726f848ce1068de73437c", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "snes9x", SHA256: "eaa0bcfce67673809886e50387a80a616b719502175db64c090d04c9d75958ee", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "mame2003", SHA256: "1d8283ce042f71607b9b55656cd4068f703c52faa7a3d0940855c9dd21d542df", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "mame2003_plus", SHA256: "cb6d9c80a88b65d1579d16d02128a678f8d1cd3f51de1479e647cea27b13247b", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "fbalpha2012_cps1", SHA256: "15b47667eb3c3746649c79e997b9f8c463f83bed9f61f51322cbe4db3d6e078e", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "fbalpha2012_cps2", SHA256: "432c2dd513603b04ccbf4e81f282f012763d2435311805443e2bd0cc9021d8d1", SupportedContentKinds: []string{"SINGLE_FILE"}},
		{CoreID: "nestopia", SHA256: "051de1b67a5b582b8a1bac6b99471d4f9f883ce3b3603d00330c1a066e546375", SupportedContentKinds: []string{"SINGLE_FILE"}},
	}
	return &dependencies.Set{Versions: map[string]*dependencies.Version{"4.2.3": version}}
}
