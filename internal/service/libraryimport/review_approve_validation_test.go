package libraryimport

import (
	"errors"
	"math"
	"testing"

	"retrom/internal/content/arcade"
	corevalidation "retrom/internal/core/validation"
)

func TestReviewApprovalScreenshotDropsOnlyUnavailableExternalBIOS(t *testing.T) {
	path, blob, usable, bad := "bios.bin", "blob", "MATCHED", "INVALID"
	snapshot := corevalidation.Snapshot{SchemaVersion: 1, Kind: "STATIC", BIOS: []corevalidation.BIOSDependency{
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{DeliveryKind: "PROVIDER_BUNDLED"}},
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{
			DeliveryKind: "EXTERNAL_FILE",
			EmulatorPath: &path,
		}, FileRecord: &blob, InstallationStatus: &usable},
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{
			DeliveryKind: "EXTERNAL_FILE",
			EmulatorPath: &path,
		}, FileRecord: &blob, InstallationStatus: &bad},
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{DeliveryKind: "EXTERNAL_FILE"}},
	}}
	encoded, err := snapshot.JSON()
	if err != nil {
		t.Fatal(err)
	}
	run := reviewApprovalRun{head: ReviewApprovalHead{DependencyJSON: string(encoded)}}
	if err := run.prepareScreenshotOverride(); err != nil {
		t.Fatal(err)
	}
	actual, err := corevalidation.ParseSnapshot(run.runtimeDependencyJSON)
	if err != nil || len(actual.BIOS) != 2 || actual.BIOS[0].DeliveryKind != "PROVIDER_BUNDLED" ||
		actual.BIOS[1].FileRecord == nil {
		t.Fatalf("snapshot=%+v err=%v", actual, err)
	}
}

func validApprovalDisc(ordinal int, size int64) ApprovalDisc {
	blob, name, index := "blob", "disc.chd", int64(ordinal)
	return ApprovalDisc{
		Ordinal: ordinal, State: "PRESENT", LogicalName: name, FileRecord: &blob,
		SourceFileRecord: &blob, SourceLogicalName: &name, SourceOrdinal: &index, SizeBytes: &size,
	}
}

func TestReviewApprovalDiscIdentityAndOverflow(t *testing.T) {
	valid := validApprovalDisc(0, 8)
	if total, err := approvalDiscTotal([]ApprovalDisc{valid, validApprovalDisc(1, 8)}); err != nil || total != 16 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	for _, name := range []string{"ordinal", "missing", "source ordinal", "source blob", "blob", "name", "size"} {
		t.Run(name, func(t *testing.T) {
			disc := valid
			switch name {
			case "ordinal":
				disc.Ordinal = 1
			case "missing":
				disc.State = "MISSING"
			case "source ordinal":
				disc.SourceOrdinal = nil
			case "source blob":
				disc.SourceFileRecord = nil
			case "blob":
				disc.FileRecord = nil
			case "name":
				name := "changed"
				disc.SourceLogicalName = &name
			case "size":
				disc.SizeBytes = nil
			}
			if total, err := approvalDiscTotal([]ApprovalDisc{disc}); !errors.Is(err, ErrInvalid) || total != 0 {
				t.Fatalf("total=%d err=%v", total, err)
			}
		})
	}
	if total, err := approvalDiscTotal([]ApprovalDisc{
		validApprovalDisc(0, math.MaxInt64),
		validApprovalDisc(1, 8),
	}); !errors.Is(err, ErrInvalid) || total != 0 {
		t.Fatalf("overflow total=%d err=%v", total, err)
	}
}

func TestReviewApprovalArcadeUsesDefaultBIOSAndExactRequiredROMs(t *testing.T) {
	selected, other := "selected", "other"
	requirements := arcade.CatalogRequirements{DefaultBIOS: &selected, ROMs: []arcade.ROMRequirement{
		{Name: "main", Status: "GOOD"},
		{Name: "undumped", Status: "NODUMP"},
		{Name: "bios", Status: "GOOD", BIOSName: &selected},
		{Name: "other", Status: "GOOD", BIOSName: &other},
	}}
	if !sameApprovalRequirementNames(arcade.SelectRequirements(requirements), []string{"main", "bios"}) {
		t.Fatal("canonical ROM requirements rejected")
	}
	for _, names := range [][]string{{"bios", "main"}, {"main"}, {"main", "bios", "other"}, {"main", "bios", "undumped"}} {
		if sameApprovalRequirementNames(arcade.SelectRequirements(requirements), names) {
			t.Fatalf("noncanonical requirements=%v", names)
		}
	}
}
