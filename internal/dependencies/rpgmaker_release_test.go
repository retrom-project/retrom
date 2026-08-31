package dependencies

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHydrateRPGMakerReleaseFilesUsesObservedCacheIdentity(t *testing.T) {
	t.Parallel()

	version := releaseTestVersion(t)
	if err := hydrateRPGMakerReleaseFiles(version); err != nil {
		t.Fatalf("hydrate release files: %v", err)
	}
	for index, file := range version.Manifest.RuntimeFiles {
		wantSize := int64(100 + index)
		wantDigest := releaseTestDigest(byte(index + 1))
		if file.SizeBytes != wantSize || file.SHA256 != wantDigest {
			t.Fatalf("file %s = %d/%s, want %d/%s", file.Path, file.SizeBytes, file.SHA256, wantSize, wantDigest)
		}
	}
}

func TestHydrateRPGMakerReleaseFilesRejectsObservedIdentityDrift(t *testing.T) {
	t.Parallel()

	version := releaseTestVersion(t)
	observedPath := filepath.Join(version.RuntimeRoot, rpgMakerObservedReleaseFilename)
	contents, err := os.ReadFile(observedPath)
	if err != nil {
		t.Fatal(err)
	}
	var observed map[string]any
	if err := json.Unmarshal(contents, &observed); err != nil {
		t.Fatal(err)
	}
	observed["tag"] = "v9.9.9"
	writeReleaseTestJSON(t, observedPath, observed)

	if err := hydrateRPGMakerReleaseFiles(version); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hydrate drifted release error = %v, want %v", err, ErrInvalid)
	}
}

func TestRPGMakerReleaseIdentityRejectsMigrationTags(t *testing.T) {
	t.Parallel()

	release := releaseTestDeclaration()
	if !validRPGMakerReleaseIdentity(release) {
		t.Fatal("clean release identity rejected")
	}
	release.Tag = "retrom-web-f2efc98-r3"
	if validRPGMakerReleaseIdentity(release) {
		t.Fatal("migration-era release tag accepted")
	}
}

func releaseTestVersion(t *testing.T) *RPGMakerVersion {
	t.Helper()
	runtimeRoot := t.TempDir()
	release := releaseTestDeclaration()
	files := []RPGMakerRuntimeFile{
		{BundlePath: "runtime/easyrpg/easyrpg-player.js", Path: "v0.2.0/easyrpg-player.js", Role: "runtime_js", MaxSizeBytes: 1 << 20},
		{BundlePath: "runtime/easyrpg/easyrpg-player.wasm", Path: "v0.2.0/easyrpg-player.wasm", Role: "runtime_wasm", MaxSizeBytes: 16 << 20},
	}
	observed := rpgMakerObservedRelease{
		SchemaVersion: 1, Repository: release.Repository, Tag: release.Tag, TagCommit: release.TagCommit,
		BundleFilename: release.BundleAsset.Filename, Files: map[string]rpgMakerObservedAsset{},
	}
	for index, file := range files {
		observed.Files[file.Path] = rpgMakerObservedAsset{
			SizeBytes: int64(100 + index), SHA256: releaseTestDigest(byte(index + 1)),
		}
	}
	writeReleaseTestJSON(t, filepath.Join(runtimeRoot, rpgMakerObservedReleaseFilename), observed)
	return &RPGMakerVersion{
		Manifest: RPGMakerManifest{Release: release, RuntimeFiles: files}, RuntimeRoot: runtimeRoot,
	}
}

func releaseTestDeclaration() RPGMakerRuntimeRelease {
	release := RPGMakerRuntimeRelease{
		Repository: "https://github.com/retrom-project/retrom-runtime",
		Tag:        "v0.2.0", TagCommit: "1111111111111111111111111111111111111111",
	}
	release.BundleAsset = RPGMakerReleaseMetadata{
		Filename: "retrom-runtime-0.2.0.tar.gz", MaxSizeBytes: 256 << 20,
		URL: rpgMakerReleaseURL(release, "retrom-runtime-0.2.0.tar.gz"),
	}
	release.MetadataAsset = RPGMakerReleaseMetadata{
		Filename: "retrom-runtime-release.json", MaxSizeBytes: 1 << 20,
		URL: rpgMakerReleaseURL(release, "retrom-runtime-release.json"),
	}
	return release
}

func releaseTestDigest(value byte) string {
	const hexadecimal = "0123456789abcdef"
	return string([]byte{hexadecimal[value%16]}) + "000000000000000000000000000000000000000000000000000000000000000"
}

func writeReleaseTestJSON(t *testing.T, target string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
