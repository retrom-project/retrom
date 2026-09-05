package runtimebundle

import (
	"errors"
	"strings"
	"testing"
)

func TestParseManifestIsClosed(t *testing.T) {
	t.Parallel()
	manifest, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProviderID != "fixture" || manifest.ProviderVersion != "1.0.0" ||
		len(manifest.Targets) != 1 || manifest.Targets[0].ID != "core" {
		t.Fatalf("manifest = %#v", manifest)
	}

	_, err = ParseManifest([]byte(strings.Replace(fixtureManifest, `"schemaVersion":1`,
		`"schemaVersion":1,"adapterId":"leaked"`, 1)))
	if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
	for _, missing := range []string{`"volume":true,`, `"checkpoint":null,`} {
		_, err = ParseManifest([]byte(strings.Replace(fixtureManifest, missing, "", 1)))
		if !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("missing field %s error = %v", missing, err)
		}
	}
}

func TestManifestIdentityAndTokenRulesMatchTheAuthority(t *testing.T) {
	t.Parallel()
	accepted := strings.Replace(fixtureManifest, `"providerId":"fixture"`, `"providerId":"fixture--provider"`, 1)
	if _, err := ParseManifest([]byte(accepted)); err != nil {
		t.Fatalf("authority-compatible identity rejected: %v", err)
	}
	for _, changed := range []string{
		strings.Replace(fixtureManifest, `"providerId":"fixture"`, `"providerId":"fixture_provider"`, 1),
		strings.Replace(fixtureManifest, `"id":"core"`, `"id":"Core"`, 1),
	} {
		if _, err := ParseManifest([]byte(changed)); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("authority-invalid manifest error = %v", err)
		}
	}
}

func TestCanonicalJSONOrdersObjectKeysByUTF16CodeUnits(t *testing.T) {
	t.Parallel()
	canonical, err := canonicalJSON([]byte(`{"\ue000":1,"\ud800\udc00":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"𐀀":2,"":1}` {
		t.Fatalf("canonical = %s", canonical)
	}
}

func TestBindTargetIntegrityRequiresEveryDeclaredAsset(t *testing.T) {
	t.Parallel()
	manifest, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	files := []IntegrityFile{{
		Path: "assets/core.wasm", SizeBytes: 1, SHA256: strings.Repeat("a", 64),
	}}
	bound, err := BindTargetIntegrity(manifest, files)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Targets[0].ID != manifest.Targets[0].ID {
		t.Fatalf("bound manifest = %#v", bound)
	}
	if _, err := BindTargetIntegrity(manifest, nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("missing target asset error = %v", err)
	}
}

func TestBindTargetIntegrityRejectsDuplicateOrInvalidFiles(t *testing.T) {
	t.Parallel()
	manifest, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	files := []IntegrityFile{{
		Path: "assets/core.wasm", SizeBytes: 1, SHA256: strings.Repeat("a", 64),
	}}
	for _, invalid := range [][]IntegrityFile{
		{files[0], files[0]},
		{{Path: "../core.wasm", SizeBytes: 1, SHA256: strings.Repeat("a", 64)}},
	} {
		if _, err := BindTargetIntegrity(manifest, invalid); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("invalid files accepted: %#v, %v", invalid, err)
		}
	}
}

func TestParseIntegrityClosesMediaAndOrdering(t *testing.T) {
	valid := `{"schemaVersion":1,"files":[` +
		`{"path":"assets/core.wasm","sizeBytes":4,"sha256":"` + strings.Repeat("a", 64) + `","mediaType":"application/wasm"},` +
		`{"path":"client.mjs","sizeBytes":8,"sha256":"` + strings.Repeat("b", 64) + `","mediaType":"text/javascript; charset=utf-8"},` +
		`{"path":"provider.json","sizeBytes":2,"sha256":"` + strings.Repeat("c", 64) + `","mediaType":"application/json; charset=utf-8"}]}`
	integrity, err := ParseIntegrity([]byte(valid))
	if err != nil || len(integrity.Files) != 3 || integrity.Files[0].MediaType != "application/wasm" {
		t.Fatalf("integrity = %#v, %v", integrity, err)
	}
	for _, invalid := range []string{
		strings.Replace(valid, "application/wasm", "video/mp4", 1),
		strings.Replace(valid, `"path":"client.mjs"`, `"path":"../client.mjs"`, 1),
		strings.Replace(valid, `"mediaType":"application/wasm"`, `"mediaType":"application/wasm","extra":true`, 1),
		strings.Replace(valid, `{"path":"assets/core.wasm"`, `{"path":"z/core.wasm"`, 1),
	} {
		if _, err := ParseIntegrity([]byte(invalid)); !errors.Is(err, ErrIntegrityInvalid) {
			t.Fatalf("invalid integrity accepted: %v", err)
		}
	}
}

const fixtureManifest = `{
  "schemaVersion":1,
  "providerId":"fixture",
  "providerVersion":"1.0.0",
  "providerApiVersion":1,
  "clientModulePath":"client.mjs",
  "targets":[{
    "id":"core",
    "displayName":"Core",
    "targetOptionsSchema":{"type":"object","additionalProperties":false,"properties":{},"required":[]},
    "inputs":[{"role":"game","kind":"ROM_BLOB","cardinality":"ONE","optional":false}],
    "capabilities":{"pause":true,"screenshot":true,"checkpoint":false,"standardGamepad":true,"frameCounter":false,"volume":true,"discSwitch":false,"nativeSettings":true,"inputFilter":true,"netplayPort":false,"videoModes":["original","pixel"],"requiresThreads":false,"frameMode":"SAME_ORIGIN_BLANK"},
    "checkpoint":null,
    "assetPaths":["assets/core.wasm"]
  }]
}`
