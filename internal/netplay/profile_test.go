package netplay

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"retrom/internal/dependencies"
	"retrom/internal/runtimecatalog"
	"retrom/internal/testassert"
)

func TestRegistryProducesStableProviderTargetCanonicalProfile(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", ManifestRelativePath))
	testassert.False(t, err != nil, err)
	registry, err := parseRegistry(contents, fixtureDependencySet())
	testassert.False(t, err != nil, err)
	profile, ok := registry.Profile("fbneo-423-v1")
	testassert.True(t, ok, "expected profile missing")
	input := CanonicalProfileInput{
		ManifestProfile: profile, TargetContractSHA256: strings.Repeat("a", 64),
		GameVariantRevisionID:  "01980000-0000-7000-8000-000000000002",
		DependencySnapshotJSON: `{"schemaVersion":1}`,
	}
	canonicalA, digestA, err := registry.CanonicalProfile(input)
	testassert.False(t, err != nil, err)
	canonicalB, digestB, err := registry.CanonicalProfile(input)
	testassert.Falsef(t, err != nil || string(canonicalA) != string(canonicalB) || digestA != digestB ||
		!validDigest(digestA), "canonical profile drift: %s/%s error=%v", digestA, digestB, err)
	input.TargetContractSHA256 = strings.Repeat("b", 64)
	_, changedDigest, err := registry.CanonicalProfile(input)
	if err != nil || changedDigest == digestA {
		t.Fatalf("Target contract identity was not bound: %s/%s error=%v", digestA, changedDigest, err)
	}
}

func TestRegistryRejectsContentSpecificAndLegacyRuntimeFields(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", ManifestRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(contents), `"maxPlayers":2`,
		`"maxPlayers":2,"contentSha256":"`+strings.Repeat("a", 64)+`"`, 1)
	if _, err := parseRegistry([]byte(mutated), fixtureDependencySet()); err == nil {
		t.Fatal("content-specific profile field accepted")
	}
	mutated = strings.Replace(string(contents), `"providerId":"emulatorjs"`,
		`"providerId":"emulatorjs","emulatorjsVersion":"4.2.3"`, 1)
	if _, err := parseRegistry([]byte(mutated), fixtureDependencySet()); err == nil {
		t.Fatal("legacy runtime field accepted")
	}
}

func TestRegistryContainsOnlyTheEightProviderTargetProfiles(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", ManifestRelativePath))
	testassert.False(t, err != nil, err)
	registry, err := parseRegistry(contents, fixtureDependencySet())
	testassert.False(t, err != nil, err)
	if len(registry.profiles) != 8 {
		t.Fatalf("profile count = %d, want 8", len(registry.profiles))
	}
	profile, ok := registry.Profile("fceumm-423-v1")
	if !ok || profile.ProviderID != "emulatorjs" || profile.TargetID != "fceumm" ||
		profile.NetplayCompatibilityLine != "emulatorjs-netplay-v2" ||
		!slices.Equal(profile.PlatformIDs, []string{"nes"}) {
		t.Fatalf("FCEUmm profile = %+v, exists=%t", profile, ok)
	}
	testassert.True(t, registry.SupportsPlatformTarget(
		"nes", "fceumm", "emulatorjs", "fceumm", "emulatorjs-netplay-v2",
	), "FCEUmm Provider Target should support netplay on NES")
	testassert.False(t, registry.SupportsPlatformTarget(
		"snes", "fceumm", "emulatorjs", "fceumm", "emulatorjs-netplay-v2",
	), "mismatched platform was marked as netplay capable")
}

func fixtureDependencySet() *dependencies.Set {
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		panic(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		panic(err)
	}
	return &dependencies.Set{RuntimeCatalog: catalog}
}
