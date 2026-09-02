package platformcatalog

import (
	"slices"
	"testing"

	"retrom/internal/contentprofile"
	"retrom/internal/testassert"
)

func TestCurrentCatalogIsValidAndReturnsDeepCopy(t *testing.T) {
	t.Parallel()
	catalog := Current()
	if err := Validate(catalog); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return catalog.Version != 8 }, func() bool { return len(catalog.Templates) != 33 }), "catalog = version:%d templates:%d", catalog.Version, len(catalog.Templates))
	catalog.Templates[0].Name = "changed"
	testassert.False(t, Current().Templates[0].Name != "NES 游戏", "Current returned mutable catalog storage")
}

func TestCatalogContainsWASM4Directory(t *testing.T) {
	t.Parallel()
	for _, template := range Current().Templates {
		if template.Key == "wasm4/wasm4" && template.Name == "WASM-4 游戏" {
			return
		}
	}
	t.Fatal("WASM-4 directory template missing")
}

func TestCatalogContainsONSDirectory(t *testing.T) {
	t.Parallel()
	for _, template := range Current().Templates {
		if template.Key == "ons/onscripter_yuri" && template.Name == "ONS 游戏" {
			return
		}
	}
	t.Fatal("ONS directory template missing")
}

func TestCatalogContainsKiriKiriDirectory(t *testing.T) {
	t.Parallel()
	for _, template := range Current().Templates {
		if template.Key == "kirikiri/kirikiri2" && template.Name == "KiriKiri 游戏" {
			return
		}
	}
	t.Fatal("KiriKiri directory template missing")
}

func TestCatalogContainsButterscotchDirectory(t *testing.T) {
	t.Parallel()
	for _, template := range Current().Templates {
		if template.Key == "butterscotch/butterscotch" && template.Name == "GameMaker 游戏" {
			return
		}
	}
	t.Fatal("Butterscotch directory template missing")
}

func TestCatalogContainsTyranoScriptDirectory(t *testing.T) {
	t.Parallel()
	for _, template := range Current().Templates {
		if template.Key == "tyranoscript/tyranoscript" && template.Name == "TyranoScript 游戏" {
			return
		}
	}
	t.Fatal("TyranoScript directory template missing")
}

func TestCatalogContainsOneVirtualRPGMakerDirectory(t *testing.T) {
	t.Parallel()
	want := []string{
		"rpgmaker/rpgmaker",
	}
	got := make([]string, 0, len(want))
	for _, template := range Current().Templates {
		if template.PlatformID == "rpgmaker" {
			got = append(got, template.Key)
		}
	}
	testassert.Truef(t, slices.Equal(got, want), "RPG Maker templates = %#v", got)
}

func TestCatalogConsolidatesFDSAndMAME2003(t *testing.T) {
	t.Parallel()
	catalog := Current()
	keys := make([]string, 0, len(catalog.Templates))
	for _, template := range catalog.Templates {
		keys = append(keys, template.Key)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return slices.Contains(keys, "fds/fceumm") }, func() bool { return slices.Contains(keys, "arcade/mame2003") }), "retired recommendations remained: %#v", keys)
	if got := contentprofile.SupportedExtensions("nes"); !slices.Equal(got, []string{".nes", ".unf", ".unif", ".fds"}) {
		t.Fatalf("NES extensions = %#v", got)
	}
	arcadeCount := 0
	for _, template := range catalog.Templates {
		if template.PlatformID == "arcade" {
			arcadeCount++
			testassert.Truef(t, slices.Equal(contentprofile.SupportedExtensions(template.PlatformID), []string{".zip"}), "Arcade extensions for %s are invalid", template.Key)
		}
	}
	testassert.Falsef(t, arcadeCount != 4, "Arcade recommendation count = %d", arcadeCount)
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
