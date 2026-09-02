package runtimecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/platformcatalog"
)

func TestParseCatalogAndRejectImplementationFacts(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != 1 || catalog.CatalogVersion != 1 || len(catalog.Bindings) != 47 {
		t.Fatalf("catalog = %#v", catalog)
	}
	for _, binding := range catalog.Bindings {
		if binding.ID == "" || binding.CoreID == "" || binding.ProviderID == "" || binding.TargetID == "" {
			t.Fatalf("incomplete binding = %#v", binding)
		}
		if binding.ProviderID == "retrom-runtime" && strings.HasPrefix(binding.TargetID, "rpgmaker-") && binding.CoreID != "rpgmaker" {
			t.Fatalf("RPG generation leaked as Product Core: %#v", binding)
		}
	}
	changed := strings.Replace(string(contents), `"catalogVersion": 1`,
		`"catalogVersion": 1, "adapterId": "leaked"`, 1)
	_, err = ParseCatalog([]byte(changed))
	if !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestPlatformDefaultsSelectBindingsOnlyByProductCore(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range platformcatalog.Current().Templates {
		matches := make([]Binding, 0)
		for _, binding := range catalog.Bindings {
			if binding.CoreID == template.DefaultCoreID && containsString(binding.PlatformIDs, template.PlatformID) {
				matches = append(matches, binding)
			}
		}
		if template.PlatformID == "rpgmaker" {
			if len(matches) != 7 {
				t.Fatalf("RPG Maker generation bindings = %d, want 7", len(matches))
			}
			continue
		}
		if len(matches) != 1 {
			t.Fatalf("default %s/%s bindings = %#v", template.PlatformID, template.DefaultCoreID, matches)
		}
	}
	for _, expected := range []struct{ platform, core, target string }{
		{platform: "gbc", core: "gambatte", target: "gambatte"},
		{platform: "nds", core: "desmume2015", target: "desmume2015"},
	} {
		found := false
		for _, binding := range catalog.Bindings {
			found = found || binding.CoreID == expected.core && binding.TargetID == expected.target &&
				containsString(binding.PlatformIDs, expected.platform)
		}
		if !found {
			t.Fatalf("missing default binding %#v", expected)
		}
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
