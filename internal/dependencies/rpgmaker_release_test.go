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
	releases, ok := observed["releases"].(map[string]any)
	if !ok {
		t.Fatal("observed releases is not an object")
	}
	mkxp, ok := releases["mkxp"].(map[string]any)
	if !ok {
		t.Fatal("observed mkxp release is not an object")
	}
	mkxp["tag"] = "retrom-web-f2efc98-r999"
	writeReleaseTestJSON(t, observedPath, observed)

	if err := hydrateRPGMakerReleaseFiles(version); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hydrate drifted release error = %v, want %v", err, ErrInvalid)
	}
}

func releaseTestVersion(t *testing.T) *RPGMakerVersion {
	t.Helper()
	runtimeRoot := t.TempDir()
	releases := []RPGMakerRuntimeRelease{
		releaseTestDeclaration(
			"easyrpg", "https://github.com/xxxsen/Player", "retrom-web-0.8.1.1-r2",
			"1111111111111111111111111111111111111111", "easyrpg-save-v1",
			"easyrpg-player.js", "easyrpg-player.wasm", "0.8.1.1-v4",
		),
		releaseTestDeclaration(
			"mkxp", "https://github.com/xxxsen/mkxp-z-libretro-emscripten", "retrom-web-f2efc98-r2",
			"2222222222222222222222222222222222222222", "mkxp-state-v1",
			"mkxp-z_libretro.js", "mkxp-z_libretro.wasm", "f2efc98-v3",
		),
		releaseTestDeclaration(
			"easyrpg-r3", "https://github.com/xxxsen/Player", "retrom-web-0.8.1.1-r3",
			"3333333333333333333333333333333333333333", "easyrpg-save-v1",
			"easyrpg-player.js", "easyrpg-player.wasm", "0.8.1.1-v5",
		),
	}
	files := make([]RPGMakerRuntimeFile, 0, 4)
	observed := rpgMakerObservedReleases{SchemaVersion: 1, Releases: map[string]rpgMakerObservedRelease{}}
	index := 0
	for _, release := range releases {
		record := rpgMakerObservedRelease{
			Repository: release.Repository, Tag: release.Tag, TagCommit: release.TagCommit,
			AdapterABI: release.AdapterABI, Assets: map[string]rpgMakerObservedAsset{},
		}
		for _, asset := range release.Assets {
			files = append(files, RPGMakerRuntimeFile{
				Path: asset.Path, ReleaseID: release.ID, AssetFilename: asset.Filename, Role: asset.Role,
			})
			record.Assets[asset.Filename] = rpgMakerObservedAsset{
				SizeBytes: int64(100 + index), SHA256: releaseTestDigest(byte(index + 1)),
			}
			index++
		}
		observed.Releases[release.ID] = record
	}
	writeReleaseTestJSON(t, filepath.Join(runtimeRoot, rpgMakerObservedReleaseFilename), observed)
	return &RPGMakerVersion{
		Manifest:    RPGMakerManifest{RuntimeFiles: files, RuntimeReleases: releases},
		RuntimeRoot: runtimeRoot,
	}
}

func releaseTestDeclaration(
	id string,
	repository string,
	tag string,
	commit string,
	abi string,
	jsFilename string,
	wasmFilename string,
	runtimeVersion string,
) RPGMakerRuntimeRelease {
	release := RPGMakerRuntimeRelease{
		ID: id, Repository: repository, Tag: tag, TagCommit: commit, AdapterABI: abi,
		BinaryAssociation: "TAGGED_RELEASE_COMPATIBLE",
	}
	release.MetadataAsset = RPGMakerReleaseMetadata{
		Filename: "retrom-runtime-release.json", MaxSizeBytes: 65536,
		URL: rpgMakerReleaseURL(release, "retrom-runtime-release.json"),
	}
	release.Assets = []RPGMakerReleaseAsset{
		{
			Filename: jsFilename, URL: rpgMakerReleaseURL(release, jsFilename),
			Path: runtimeVersion + "/" + jsFilename, Role: "runtime_js", MaxSizeBytes: 1 << 20,
		},
		{
			Filename: wasmFilename, URL: rpgMakerReleaseURL(release, wasmFilename),
			Path: runtimeVersion + "/" + wasmFilename, Role: "runtime_wasm", MaxSizeBytes: 64 << 20,
		},
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
