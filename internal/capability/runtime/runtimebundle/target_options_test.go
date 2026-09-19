package runtimebundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runtimejson "retrom/internal/capability/runtime/runtimejson"
	runtimecontract "retrom/internal/model/runtimecontract"
)

func TestSharedTargetOptionsSchemaFixtures(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "runtime-provider", "v1", "fixtures",
		"target-options", "schema-validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := runtimejson.ParseStrictJSON(contents)
	if err != nil {
		t.Fatal(err)
	}
	root, rootOK := parsed.(map[string]any)
	schemaValue, schemaOK := root["schema"].(map[string]any)
	cases, casesOK := root["cases"].([]any)
	if !rootOK || !schemaOK || !casesOK {
		t.Fatal("shared target options fixture shape is invalid")
	}
	schemaContents, err := json.Marshal(schemaValue)
	if err != nil {
		t.Fatal(err)
	}
	var schema runtimecontract.TargetOptionsSchema
	if err := json.Unmarshal(schemaContents, &schema); err != nil {
		t.Fatal(err)
	}
	if !runtimejson.ValidateTargetOptionsSchema(schema) {
		t.Fatal("shared target options schema is invalid")
	}
	for index, candidate := range cases {
		item, itemOK := candidate.(map[string]any)
		value, valueOK := item["value"].(map[string]any)
		expected, expectedOK := item["valid"].(bool)
		if !itemOK || !valueOK || !expectedOK {
			t.Fatalf("case %d shape is invalid", index)
		}
		valueContents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		valid := runtimejson.ValidateTargetOptions(schema, valueContents)
		if valid != expected {
			t.Fatalf("case %d validity = %t", index, valid)
		}
	}
}
