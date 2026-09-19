package runtimecontract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/capability/runtime/runtimecatalog"
	"retrom/internal/model/runtimecontract"
)

type catalogGolden struct {
	RawCatalogSHA256        string
	NormalizedCatalogJSON   string
	NormalizedCatalogSHA256 string
	Samples                 map[string]string
}

func readCatalogGolden(t *testing.T) catalogGolden {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "catalog-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden catalogGolden
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	return golden
}

func TestCatalogSerializationMatchesBaseline(t *testing.T) {
	t.Parallel()
	golden := readCatalogGolden(t)
	catalogPath := filepath.Join("..", "..", "..", "data", "runtime-target-bindings", "v1", "catalog.json")
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if digestCatalogBytes(raw) != golden.RawCatalogSHA256 {
		t.Fatal("source catalog fixture changed")
	}
	catalog, err := runtimecatalog.ParseCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	assertCatalogGolden(t, catalog, golden)
}

func assertCatalogGolden(t *testing.T, catalog runtimecontract.Catalog, golden catalogGolden) {
	t.Helper()
	actual, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	digest := digestCatalogBytes(actual)
	if string(actual) != golden.NormalizedCatalogJSON || digest != golden.NormalizedCatalogSHA256 {
		t.Fatalf("catalog serialization or projection digest changed: digest=%s, want=%s",
			digest, golden.NormalizedCatalogSHA256)
	}
	var roundTrip runtimecontract.Catalog
	if err := json.Unmarshal(actual, &roundTrip); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(roundTrip)
	if err != nil || string(encoded) != golden.NormalizedCatalogJSON {
		t.Fatalf("catalog round trip changed: error=%v", err)
	}
}

func TestCatalogValueShapesPreserveBaselineJSON(t *testing.T) {
	t.Parallel()
	golden := readCatalogGolden(t)
	samples := map[string]any{
		"zero-catalog": runtimecontract.Catalog{},
		"empty-catalog": runtimecontract.Catalog{
			Definitions: runtimecontract.Definitions{
				Platforms:    []runtimecontract.PlatformDefinition{},
				Cores:        []runtimecontract.CoreDefinition{},
				ContentKinds: []string{}, AssetPacks: []runtimecontract.AssetPackDefinition{},
			},
			Bindings: []runtimecontract.Binding{},
		},
		"zero-binding":  runtimecontract.Binding{},
		"empty-binding": runtimecontract.Binding{PlatformIDs: []string{}, AcceptedContentKinds: []string{}},
		"asset-pack-unicode": runtimecontract.AssetPackDefinition{
			ID: "extra-assets", Kind: "ADDITIONAL_ASSETS", Generation: "RPGXP", DeclaredName: "Ｓtraße",
			NormalizedDeclaredName: "strasse", DisplayName: "Extra <&> assets",
			RequiredLayoutVersion: "mkxpz-v1", Enabled: true,
		},
		"platform": runtimecontract.PlatformDefinition{ID: "platform", Name: "Retrom <&> 游戏", SortOrder: 7},
		"core":     runtimecontract.CoreDefinition{ID: "core", Name: "Core", Enabled: true},
	}
	if len(samples) != len(golden.Samples) {
		t.Fatal("golden sample set changed")
	}
	for name, value := range samples {
		t.Run(name, func(t *testing.T) {
			actual, err := json.Marshal(value)
			if err != nil || string(actual) != golden.Samples[name] {
				t.Fatalf("value JSON = %s, want %s; error=%v", actual, golden.Samples[name], err)
			}
		})
	}
}

func digestCatalogBytes(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
