package runtimebundle

import (
	"errors"
	"strings"
	"testing"
)

func TestParseManifestClosedAndProjectsTargetDigest(t *testing.T) {
	t.Parallel()
	manifest, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProviderID != "fixture" || manifest.ProviderVersion != "1.0.0" ||
		len(manifest.Targets) != 1 || manifest.Targets[0].ID != "core" ||
		manifest.Targets[0].ContractSHA256 != "" {
		t.Fatalf("manifest = %#v", manifest)
	}

	_, err = ParseManifest([]byte(strings.Replace(fixtureManifest, `"schemaVersion":1`,
		`"schemaVersion":1,"adapterId":"leaked"`, 1)))
	if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
	for _, missing := range []string{`"volume":true,`, `"netplayCompatibilityLine":null,`, `"checkpoint":null,`} {
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
		strings.Replace(fixtureManifest, `"gameCompatibilityLine":"core-v1"`, `"gameCompatibilityLine":"Core-v1"`, 1),
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

func TestTargetContractDigestIncludesEveryDeclaredAssetDigest(t *testing.T) {
	t.Parallel()
	manifest, err := ParseManifest([]byte(fixtureManifest))
	if err != nil {
		t.Fatal(err)
	}
	files := []IntegrityFile{{
		Path: "assets/core.wasm", SizeBytes: 1, SHA256: strings.Repeat("a", 64),
	}}
	first, err := BindTargetIntegrity(manifest, files)
	if err != nil {
		t.Fatal(err)
	}
	files[0].SHA256 = strings.Repeat("b", 64)
	second, err := BindTargetIntegrity(manifest, files)
	if err != nil {
		t.Fatal(err)
	}
	if first.Targets[0].ContractSHA256 == second.Targets[0].ContractSHA256 {
		t.Fatal("target contract digest ignored changed asset bytes")
	}
	if _, err := BindTargetIntegrity(manifest, nil); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("missing target asset error = %v", err)
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
    "gameCompatibilityLine":"core-v1",
    "netplayCompatibilityLine":null,
    "optionsKind":"NONE_V1",
    "inputs":[{"role":"game","kind":"ROM_BLOB_V1","cardinality":"ONE","optional":false}],
    "capabilities":{"pause":true,"screenshot":true,"checkpoint":false,"standardGamepad":true,"frameCounter":false,"volume":true,"discSwitch":false,"nativeSettings":true,"inputFilter":true,"netplayPort":false,"videoModes":["original","pixel"],"requiresThreads":false,"frameMode":"SAME_ORIGIN_BLANK","validationProbes":[]},
    "checkpoint":null,
    "assetPaths":["assets/core.wasm"]
  }]
}`
