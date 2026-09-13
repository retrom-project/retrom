package firmwaremanifest

import (
	"strings"
	"testing"
)

func TestPinnedCatalogDeclaresArchiveSlotsAndDefaultBIOS(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source.CoreID != "same_cdi" || len(catalog.Items) != 3 {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	modes := map[string]string{"cdimono1.zip": "REQUIRED", "cdimono2.zip": "OPTIONAL", "cdibios.zip": "OPTIONAL"}
	counts := map[string]int{"cdimono1.zip": 3, "cdimono2.zip": 3, "cdibios.zip": 2}
	for _, item := range catalog.Items {
		if item.Mode != modes[item.LogicalName] || item.EmulatorPath != "/same_cdi/bios/"+item.LogicalName {
			t.Fatalf("unexpected firmware: %#v", item)
		}
		required := 0
		for _, member := range item.Members {
			if member.Required {
				required++
			}
		}
		if required != counts[item.LogicalName] {
			t.Errorf("%s required members=%d", item.LogicalName, required)
		}
		delete(modes, item.LogicalName)
	}
	if len(modes) != 0 {
		t.Fatalf("missing slots: %v", modes)
	}
}

func TestMemberDeclarationRejectsUnsafeAndUnverifiableValues(t *testing.T) {
	base := Member{Name: "boot.rom", SizeBytes: 16, CRC32: "12345678", SHA1: strings.Repeat("a", 40), Required: true}
	for _, name := range []string{".", "..", "../boot.rom", "/boot.rom", "a\\boot.rom", "a/../boot.rom", "bad\x00.rom"} {
		t.Run(name, func(t *testing.T) {
			value := base
			value.Name = name
			if err := ValidateMembers([]Member{value}); err == nil {
				t.Fatal("unsafe member accepted")
			}
		})
	}
	for _, members := range [][]Member{nil, {base, base}, {{Name: "boot.rom", SizeBytes: 16, CRC32: "12345678", SHA1: strings.Repeat("x", 40), Required: true}}, {{Name: "boot.rom", SizeBytes: 16, CRC32: "12345678", SHA1: strings.Repeat("a", 40)}}} {
		if err := ValidateMembers(members); err == nil {
			t.Fatalf("invalid members accepted: %#v", members)
		}
	}
}
