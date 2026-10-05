package arcade

import (
	"context"
	"testing"

	"retrom/internal/importing"
)

type targetDATCatalog struct{}

func (targetDATCatalog) MachineClassification(context.Context, string, string) (string, bool, error) {
	return "NORMAL", true, nil
}

func (targetDATCatalog) MachineRelation(_ context.Context, _, machine string) (MachineRelation, bool, error) {
	if machine == "child" {
		return MachineRelation{CloneOf: "parent", ROMOf: "bios"}, true, nil
	}
	return MachineRelation{}, machine == "parent" || machine == "bios", nil
}

func (targetDATCatalog) ArcadeRequirements(_ context.Context, dat, machine string) (CatalogRequirements, error) {
	crc := "good"
	if dat == "different-core" && machine == "child" {
		crc = "different"
	}
	return CatalogRequirements{ROMs: []ROMRequirement{{Name: machine + ".bin", Size: 1, CRC32: &crc, Status: "GOOD"}}}, nil
}

type archiveObservations map[string][]importing.ArchiveEntry

func (observations archiveObservations) Entries(_ context.Context, record string) ([]importing.ArchiveEntry, error) {
	return observations[record], nil
}

func TestPublishedPreparationUsesSelectedTargetDATAndKeepsBIOSGapSeparate(t *testing.T) {
	preparation := NewPreparation(targetDATCatalog{}, archiveObservations{
		"child-record":     {{NormalizedPath: "child.bin", Size: 1, CRC32: "good"}},
		"parent-record":    {{NormalizedPath: "parent.bin", Size: 1, CRC32: "good"}},
		"unrelated-record": {{NormalizedPath: "other.bin", Size: 1, CRC32: "good"}},
	})
	files := []SourceFile{{Role: "CONTENT", LogicalName: "child.zip", FileRecord: "child-record"}, {Role: "COMPANION", LogicalName: "parent.zip", FileRecord: "parent-record"}, {Role: "COMPANION", LogicalName: "unrelated.zip", FileRecord: "unrelated-record"}}
	for _, test := range []struct{ dat, code string }{{"source-core", "LAUNCH_BIOS_MISSING"}, {"different-core", "ARCADE_CONTENT_MISMATCH"}} {
		result, err := preparation.Prepare(t.Context(), files, test.dat, "child", "child.zip")
		if err != nil || result.Status != "BLOCKED" || result.Code != test.code || result.Snapshot.DatVersionID != test.dat || len(result.Companions) != 1 || result.Companions[0].LogicalName != "parent.zip" {
			t.Fatalf("selected DAT %s: result=%+v error=%v", test.dat, result, err)
		}
	}
	result, err := preparation.Prepare(t.Context(), files, "source-core", "child", "unrelated.zip")
	if err != nil || result.Code != "ARCADE_CONTENT_MISSING_ENTRY" {
		t.Fatalf("unrelated source became primary: %+v %v", result, err)
	}
}
