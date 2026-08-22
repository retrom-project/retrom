package platformcatalog

import (
	"slices"
	"testing"

	"retrom/internal/contentprofile"
)

func TestCurrentCatalogIsValidAndReturnsDeepCopy(t *testing.T) {
	t.Parallel()
	catalog := Current()
	if err := Validate(catalog); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if catalog.Version != 1 || len(catalog.Templates) != 27 {
		t.Fatalf("catalog = version:%d templates:%d", catalog.Version, len(catalog.Templates))
	}
	catalog.Templates[0].Name = "changed"
	if Current().Templates[0].Name != "NES 游戏" {
		t.Fatal("Current returned mutable catalog storage")
	}
}

func TestCatalogConsolidatesFDSAndMAME2003(t *testing.T) {
	t.Parallel()
	catalog := Current()
	keys := make([]string, 0, len(catalog.Templates))
	for _, template := range catalog.Templates {
		keys = append(keys, template.Key)
	}
	if slices.Contains(keys, "fds/fceumm") || slices.Contains(keys, "arcade/mame2003") {
		t.Fatalf("retired recommendations remained: %#v", keys)
	}
	if got := contentprofile.SupportedExtensions("nes"); !slices.Equal(got, []string{".nes", ".unf", ".unif", ".fds"}) {
		t.Fatalf("NES extensions = %#v", got)
	}
	arcadeCount := 0
	for _, template := range catalog.Templates {
		if template.PlatformID == "arcade" {
			arcadeCount++
			if !slices.Equal(contentprofile.SupportedExtensions(template.PlatformID), []string{".zip"}) {
				t.Fatalf("Arcade extensions for %s are invalid", template.Key)
			}
		}
	}
	if arcadeCount != 4 {
		t.Fatalf("Arcade recommendation count = %d", arcadeCount)
	}
}

func TestValidateRejectsDuplicateAndMalformedTemplates(t *testing.T) {
	t.Parallel()
	tests := []Catalog{
		{Version: 1, Templates: []DirectoryTemplate{{Key: "nes/wrong", PlatformID: "nes", DefaultCoreID: "fceumm", Name: "NES", CatalogOrder: 10}}},
		{Version: 1, Templates: []DirectoryTemplate{{Key: "nes/fceumm", PlatformID: "nes", DefaultCoreID: "fceumm", Name: "NES", CatalogOrder: 10}, {Key: "nes/fceumm", PlatformID: "nes", DefaultCoreID: "fceumm", Name: "NES 2", CatalogOrder: 20}}},
		{Version: 1, Templates: []DirectoryTemplate{{Key: "unknown/core", PlatformID: "unknown", DefaultCoreID: "core", Name: "Unknown", CatalogOrder: 10}}},
	}
	for _, catalog := range tests {
		if err := Validate(catalog); err == nil {
			t.Fatalf("Validate(%#v) succeeded", catalog)
		}
	}
}
