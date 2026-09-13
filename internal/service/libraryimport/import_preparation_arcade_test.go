package libraryimport

import (
	"reflect"
	"testing"

	"retrom/internal/capability/format/importing"
)

func TestArcadePreparationFiltersDefaultBIOSAndRetainsHashRequirements(t *testing.T) {
	t.Parallel()
	defaultBIOS, alternative := "default", "alternative"
	crc, sha, merge := "ABCD", "SHA", "parent.bin"
	selected := SelectedArcadeRequirements(ArcadeCatalogRequirements{
		DefaultBIOS: &defaultBIOS,
		ROMs: []ArcadeROMRequirement{
			{Name: "base.bin", Status: "GOOD", Size: 3, CRC32: &crc, SHA1: &sha, MergeName: &merge},
			{Name: "default.bin", Status: "GOOD", BIOSName: &defaultBIOS},
			{Name: "other.bin", Status: "GOOD", BIOSName: &alternative},
			{Name: "unknown.bin", Status: "NODUMP"},
		},
	})
	if len(selected) != 2 || selected[0].Name != "base.bin" || selected[1].Name != "default.bin" || selected[0].MergeName != &merge {
		t.Fatalf("selected=%+v", selected)
	}
	matched := map[string]importing.ArchiveEntry{"BASE.BIN": {Size: 3, CRC32: "abcd", SHA1: "sha"}, "default.bin": {}}
	missing, mismatched, warnings := MatchArcadeRequirements(matched, selected)
	if len(missing) != 0 || len(mismatched) != 0 || len(warnings) != 0 {
		t.Fatalf("matching requirements failed: missing=%v mismatch=%v warnings=%v", missing, mismatched, warnings)
	}
}

func TestArcadePreparationHashMismatchAndBadDumpRemainSeparate(t *testing.T) {
	t.Parallel()
	crc := "abcd"
	requirements := []ArcadeROMRequirement{{Name: "absent.bin", Status: "GOOD"}, {Name: "bad.bin", Status: "BADDUMP", Size: 2, CRC32: &crc}}
	missing, mismatched, warnings := MatchArcadeRequirements(map[string]importing.ArchiveEntry{"bad.bin": {Size: 2, CRC32: "ffff"}}, requirements)
	if !reflect.DeepEqual(missing, []string{"absent.bin"}) || !reflect.DeepEqual(mismatched, []string{"bad.bin"}) || !reflect.DeepEqual(warnings, []string{"bad.bin"}) {
		t.Fatalf("missing=%v mismatch=%v warnings=%v", missing, mismatched, warnings)
	}
	selected := SelectedArcadeRequirements(ArcadeCatalogRequirements{ROMs: []ArcadeROMRequirement{{Name: "optional.bin", BIOSName: &crc}, {Name: "direct.bin", Status: "GOOD"}}})
	if len(selected) != 1 || selected[0].Name != "direct.bin" {
		t.Fatalf("unset default selected optional BIOS: %+v", selected)
	}
}
