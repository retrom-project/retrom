package arcade

import (
	"context"
	"testing"

	validation "retrom/internal/core/validation"
	"retrom/internal/importing"
)

type splitCatalog struct{}

func (splitCatalog) MachineClassification(context.Context, string, string) (string, bool, error) {
	return "NORMAL", true, nil
}

func (splitCatalog) MachineRelation(_ context.Context, _, machine string) (MachineRelation, bool, error) {
	switch machine {
	case "child":
		return MachineRelation{CloneOf: "parent", ROMOf: "parent"}, true, nil
	case "parent":
		return MachineRelation{ROMOf: "bios"}, true, nil
	case "bios":
		return MachineRelation{}, true, nil
	default:
		return MachineRelation{}, false, nil
	}
}

func (splitCatalog) ArcadeRequirements(_ context.Context, dat, machine string) (CatalogRequirements, error) {
	crc, merge := "aabbccdd", "firmware.bin"
	if dat == "other" {
		crc = "11223344"
	}
	roms := []ROMRequirement{{Name: machine + ".bin", Status: "GOOD", Size: 1, CRC32: &crc}}
	if machine == "bios" {
		roms[0].Name = merge
	}
	if machine == "parent" {
		roms = append(roms, ROMRequirement{Name: "firmware-alias.bin", Status: "GOOD", Size: 1, CRC32: &crc, MergeName: &merge})
	}
	return CatalogRequirements{ROMs: roms}, nil
}

type splitBIOS struct{ installed bool }

func (bios splitBIOS) BIOS(_ context.Context, provider, target, name string) (validation.BIOSDependency, bool, error) {
	status := "MATCHED"
	return validation.BIOSDependency{InstallationStatus: &status}, bios.installed && provider == "provider" && target == "target" && name == "bios.zip", nil
}

func splitArchives(extra bool) []Archive {
	archives := []Archive{
		{Role: "CONTENT", LogicalName: "child.zip", Entries: map[string]importing.ArchiveEntry{"child.bin": {Size: 1, CRC32: "aabbccdd"}}},
		{Role: "COMPANION", LogicalName: "parent.zip", Entries: map[string]importing.ArchiveEntry{"parent.bin": {Size: 1, CRC32: "aabbccdd"}}},
	}
	if extra {
		archives[1].Entries["firmware-alias.bin"] = importing.ArchiveEntry{Size: 1, CRC32: "aabbccdd"}
	}
	return archives
}

func TestSplitParentDelegatesInheritedBIOSWithoutRequiringDuplicateBytes(t *testing.T) {
	for _, test := range []struct {
		name             string
		installed, extra bool
		status, code     string
	}{
		{"installed BIOS", true, false, "READY", "READY"},
		{"missing BIOS", false, false, "BLOCKED", "LAUNCH_BIOS_MISSING"},
		{"safe full parent", true, true, "READY", "READY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(t.Context(), splitCatalog{}, splitBIOS{test.installed}, "provider", "target", "dat", "child", splitArchives(test.extra))
			if err != nil || result.Status != test.status || result.Code != test.code {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			for _, dep := range result.Snapshot.Dependencies {
				if dep.Kind == "PARENT" && (dep.State != "SATISFIED_EXTERNAL" || dep.RequiredEntryCount != 1) {
					t.Fatalf("parent=%+v", dep)
				}
			}
		})
	}
}

func TestSplitParentStillRejectsMissingOrCorruptOwnedAndIncludedInheritedROMs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]Archive)
	}{
		{"owned missing", func(archives []Archive) { delete(archives[1].Entries, "parent.bin") }},
		{"owned hash", func(archives []Archive) {
			archives[1].Entries["parent.bin"] = importing.ArchiveEntry{Size: 1, CRC32: "bad"}
		}},
		{"included inherited hash", func(archives []Archive) {
			archives[1].Entries["firmware-alias.bin"] = importing.ArchiveEntry{Size: 1, CRC32: "bad"}
		}},
		{"nested owned", func(archives []Archive) {
			delete(archives[1].Entries, "parent.bin")
			archives[1].Entries["nested/parent.bin"] = importing.ArchiveEntry{Size: 1, CRC32: "aabbccdd"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			archives := splitArchives(false)
			test.change(archives)
			result, err := Resolve(t.Context(), splitCatalog{}, splitBIOS{true}, "provider", "target", "dat", "child", archives)
			if err != nil || result.Code != "ARCADE_DEPENDENCY_MISMATCH" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestSplitParentReadsSelectedTargetAndDAT(t *testing.T) {
	for _, test := range []struct{ target, dat, code string }{
		{"other", "dat", "LAUNCH_BIOS_MISSING"},
		{"target", "other", "ARCADE_DEPENDENCY_MISMATCH"},
	} {
		result, err := Resolve(t.Context(), splitCatalog{}, splitBIOS{true}, "provider", test.target, test.dat, "child", splitArchives(false))
		if err != nil || result.Status != "BLOCKED" || result.Code != test.code {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
}

type unprovenMergeCatalog struct {
	splitCatalog
	sourceCRC string
}

func (c unprovenMergeCatalog) ArcadeRequirements(ctx context.Context, dat, machine string) (CatalogRequirements, error) {
	facts, err := c.splitCatalog.ArcadeRequirements(ctx, dat, machine)
	if machine == "bios" {
		facts.ROMs[0].CRC32 = &c.sourceCRC
	}
	return facts, err
}

func TestUnknownOrMismatchedMergeCannotRemoveAnArchiveRequirement(t *testing.T) {
	requirements, err := LoadRequirements(t.Context(), unprovenMergeCatalog{sourceCRC: "11223344"}, "dat", "parent")
	if err != nil || len(requirements.Owned) != 2 || len(requirements.Inherited) != 0 {
		t.Fatalf("requirements=%+v error=%v", requirements, err)
	}
}
