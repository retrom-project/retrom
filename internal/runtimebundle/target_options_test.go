package runtimebundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedTargetOptionsSchemaFixtures(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "api", "runtime-provider", "v1", "fixtures",
		"target-options", "schema-validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseStrictJSON(contents)
	if err != nil {
		t.Fatal(err)
	}
	root, rootOK := parsed.(map[string]any)
	schemaValue, schemaOK := root["schema"].(map[string]any)
	cases, casesOK := root["cases"].([]any)
	if !rootOK || !schemaOK || !casesOK {
		t.Fatal("shared target options fixture shape is invalid")
	}
	schema := TargetOptionsSchema(schemaValue)
	if !validTargetOptionsSchema(schema, 0, true) {
		t.Fatal("shared target options schema is invalid")
	}
	for index, candidate := range cases {
		item, itemOK := candidate.(map[string]any)
		value, valueOK := item["value"].(map[string]any)
		expected, expectedOK := item["valid"].(bool)
		if !itemOK || !valueOK || !expectedOK {
			t.Fatalf("case %d shape is invalid", index)
		}
		valid := ValidateTargetOptions(schema, value)
		if valid != expected {
			t.Fatalf("case %d validity = %t", index, valid)
		}
	}
}
