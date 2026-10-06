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

func TestCartridgeMemberIdentityIsIndependentOfDumpFilename(t *testing.T) {
	crc, other := "12345678", "87654321"
	policy := Policy{Kind: FlycastCartridge, Platform: "naomi", CatalogFacts: &FlycastCatalog{Machines: []FlycastMachine{
		{Name: "cart", Platform: "naomi", MediaType: "CARTRIDGE", Files: []ROMFile{{Name: "first.bin", SizeBytes: 4, CRC32: &crc}, {Name: "second.bin", SizeBytes: 4, CRC32: &other}}},
	}}}
	facts := Facts{Archive: []ArchiveMember{{Name: "second.bin", SizeBytes: 4, CRC32: crc}, {Name: "first.bin", SizeBytes: 4, CRC32: other}}}
	if got := policy.Evaluate(facts, "cart.zip"); got != nil {
		t.Fatalf("CRC-identical renamed dumps rejected: %+v", got)
	}
	facts.Archive[0].Name = "arbitrary.bin"
	if got := policy.Evaluate(facts, "cart.zip"); got != nil {
		t.Fatalf("renamed dump rejected: %+v", got)
	}
}

func TestCartridgeMatchingKeepsSizeChecksumAndOptionalBoundaries(t *testing.T) {
	crc := "12345678"
	for _, test := range []struct {
		name    string
		file    ROMFile
		members []ArchiveMember
		code    string
	}{
		{"crc_without_name", ROMFile{Name: "expected", SizeBytes: 4, CRC32: &crc}, []ArchiveMember{{Name: "renamed", SizeBytes: 4, CRC32: crc}}, ""},
		{"equivalent_duplicates", ROMFile{Name: "expected", SizeBytes: 4, CRC32: &crc}, []ArchiveMember{{Name: "a", SizeBytes: 4, CRC32: crc}, {Name: "b", SizeBytes: 4, CRC32: crc}}, ""},
		{"ambiguous_size", ROMFile{Name: "expected", SizeBytes: 4, CRC32: &crc}, []ArchiveMember{{Name: "a", SizeBytes: 4, CRC32: crc}, {Name: "b", SizeBytes: 3, CRC32: crc}}, "FLYCAST_ROM_MISMATCH"},
		{"wrong_crc_same_name", ROMFile{Name: "expected", SizeBytes: 4, CRC32: &crc}, []ArchiveMember{{Name: "expected", SizeBytes: 4, CRC32: "87654321"}}, "FLYCAST_ROM_MISMATCH"},
		{"no_crc_requires_name", ROMFile{Name: "expected", SizeBytes: 4}, []ArchiveMember{{Name: "renamed", SizeBytes: 4}}, "FLYCAST_ARCHIVE_INCOMPLETE"},
		{"no_crc_checks_size", ROMFile{Name: "expected", SizeBytes: 4}, []ArchiveMember{{Name: "expected", SizeBytes: 3}}, "FLYCAST_ROM_MISMATCH"},
		{"optional_absent", ROMFile{Name: "optional", SizeBytes: 4, Optional: true}, nil, ""},
		{"optional_present_invalid", ROMFile{Name: "optional", SizeBytes: 4, Optional: true}, []ArchiveMember{{Name: "optional", SizeBytes: 3}}, "FLYCAST_ROM_MISMATCH"},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := Policy{Kind: FlycastCartridge, Platform: "naomi", CatalogFacts: &FlycastCatalog{Machines: []FlycastMachine{{Name: "cart", Platform: "naomi", MediaType: "CARTRIDGE", Files: []ROMFile{test.file}}}}}
			got := policy.Evaluate(Facts{Archive: test.members}, "cart.zip")
			if test.code == "" {
				if got != nil {
					t.Fatalf("unexpected rejection: %+v", got)
				}
				return
			}
			if got == nil || got.Code != test.code {
				t.Fatalf("got %+v, want %s", got, test.code)
			}
		})
	}
}
