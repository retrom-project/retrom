package requirements

import (
	"testing"
)

func TestCartridgeAdmissionUsesExactCatalogAndDeliveredMembers(t *testing.T) {
	crc := "12345678"
	catalog := &FlycastCatalog{Machines: []FlycastMachine{
		{Name: "cart", Platform: "naomi", MediaType: "CARTRIDGE", Files: []ROMFile{{Name: "game.bin", SizeBytes: 4, CRC32: &crc}}},
		{Name: "split", Parent: stringPointer("cart"), Platform: "naomi", MediaType: "CARTRIDGE", Files: []ROMFile{{Name: "game.bin", SizeBytes: 4, CRC32: &crc}}},
		{Name: "disc", Platform: "naomi", MediaType: "GDROM", Disc: stringPointer("gdl-0005")},
	}}
	policy := Policy{Kind: FlycastCartridge, Platform: "naomi", CatalogFacts: catalog}
	valid := Facts{Archive: []ArchiveMember{{Name: "game.bin", SizeBytes: 4, CRC32: crc}}}
	cases := []struct {
		name, code string
		facts      Facts
	}{
		{"cart.zip", "", valid},
		{"split.zip", "", valid},
		{"disc.zip", "FLYCAST_GDROM_UNSUPPORTED", valid},
		{"unknown.zip", "FLYCAST_MACHINE_UNKNOWN", valid},
		{"split.zip", "FLYCAST_ARCHIVE_INCOMPLETE", Facts{Archive: []ArchiveMember{}}},
		{"cart.zip", "FLYCAST_ROM_MISMATCH", Facts{Archive: []ArchiveMember{{Name: "game.bin", SizeBytes: 3, CRC32: crc}}}},
	}
	for _, test := range cases {
		t.Run(test.name+test.code, func(t *testing.T) {
			got := policy.Evaluate(test.facts, test.name)
			if test.code == "" {
				if got != nil {
					t.Fatalf("complete cartridge rejected: %+v", got)
				}
			} else if got == nil || got.Code != test.code {
				t.Fatalf("got %+v, want %s", got, test.code)
			}
		})
	}
	policy.Platform = "atomiswave"
	if got := policy.Evaluate(valid, "cart.zip"); got == nil || got.Code != "FLYCAST_PLATFORM_MISMATCH" {
		t.Fatalf("wrong hardware accepted: %+v", got)
	}
	policy.CatalogFacts = nil
	if got := policy.Evaluate(valid, "cart.zip"); got == nil || got.Code != "CONTENT_REQUIREMENTS_UNAVAILABLE" {
		t.Fatalf("absent catalog accepted: %+v", got)
	}
}
func stringPointer(value string) *string { return &value }
