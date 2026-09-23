package runtimecatalog

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCatalogRejectsRetiredAssetPackDeclarations(t *testing.T) {
	contents, err := os.ReadFile("../../data/runtime-target-bindings/v1/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(contents, &catalog); err != nil {
		t.Fatal(err)
	}
	definitions, ok := catalog["definitions"].(map[string]any)
	if !ok {
		t.Fatal("catalog definitions missing")
	}
	definitions["assetPacks"] = []any{}
	contents, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCatalog(contents); err == nil {
		t.Fatal("retired asset pack declarations remain accepted")
	}
}
