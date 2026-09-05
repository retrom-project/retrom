package runtimecatalog

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBindingSelectsStrategyWithoutRepeatingFixedFacts(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("../../data/runtime-target-bindings/v1/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		SchemaVersion int                          `json:"schemaVersion"`
		Definitions   json.RawMessage              `json:"definitions"`
		Bindings      []map[string]json.RawMessage `json:"bindings"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	for _, binding := range document.Bindings {
		delete(binding, "deliveryProfile")
		delete(binding, "reviewPolicy")
	}
	minimal, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCatalog(minimal)
	if err != nil {
		t.Fatalf("strategy-selected binding rejected: %v", err)
	}
	for _, binding := range catalog.Bindings {
		strategy, ok := Strategy(binding.DetectorProfile)
		if !ok || strategy.Delivery == "" || strategy.Options == "" {
			t.Fatalf("incomplete strategy for %s: %#v", binding.ID, strategy)
		}
		encoded, err := json.Marshal(binding)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"deliveryProfile", "reviewPolicy", "optionsKind"} {
			if _, exists := fields[name]; exists {
				t.Fatalf("binding %s repeats fixed strategy field %s", binding.ID, name)
			}
		}
	}
}
