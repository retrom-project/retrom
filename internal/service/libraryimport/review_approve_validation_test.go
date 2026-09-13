package libraryimport

import (
	"context"
	"errors"
	"math"
	"testing"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/corevalidation"
)

type approvalValidationStub struct {
	ReviewValidationReader
	evidence ReviewValidationEvidence
	cause    error
}

func (stub approvalValidationStub) Evidence(context.Context, string) (ReviewValidationEvidence, error) {
	return stub.evidence, stub.cause
}

func approvalCurrentEvidence() ReviewValidationEvidence {
	value := ReviewValidationEvidence{SourceSnapshotID: "snapshot", DraftSnapshotID: "snapshot", PlatformInstanceID: "platform", DraftPlatformInstanceID: "platform", CoreID: "core", CurrentCoreID: "core", ProviderID: "provider", TargetID: "target", ManifestDigest: "manifest", SnapshotManifestDigest: "manifest", ContentKind: "SINGLE_FILE", ContentPolicy: contentcapability.NewPolicy("SINGLE_FILE"), Status: "BLOCKED", CompatibilityCode: "BIOS_MISSING", DependencyJSON: `{"schemaVersion":1,"kind":"STATIC","bios":[]}`}
	input, _ := value.CurrentInput()
	value.InputDigest = PrepublishDigest(input)
	return value
}

func TestReviewApprovalScreenshotStillRequiresCurrentValidation(t *testing.T) {
	screenshot := "runtime-proof"
	evidence := approvalCurrentEvidence()
	for _, stale := range []bool{false, true} {
		reader := approvalValidationStub{evidence: evidence}
		if stale {
			reader.evidence.InputDigest = "stale"
		}
		run := reviewApprovalRun{ctx: t.Context(), scope: ReviewApprovalScope{Validation: reader}, head: ReviewApprovalHead{ValidationID: "validation", ValidationStatus: "BLOCKED", ScreenshotID: &screenshot, DependencyJSON: evidence.DependencyJSON}}
		err := run.prepareValidation()
		if stale {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("stale screenshot error=%v", err)
			}
		} else if err != nil || !run.screenshotOverride || run.runtimeDependencyJSON != evidence.DependencyJSON {
			t.Fatalf("valid override=%v runtime=%s err=%v", run.screenshotOverride, run.runtimeDependencyJSON, err)
		}
	}
}

func TestReviewApprovalCurrentValidationPreservesFailure(t *testing.T) {
	cause := errors.New("current validation read failed")
	run := reviewApprovalRun{ctx: t.Context(), scope: ReviewApprovalScope{Validation: approvalValidationStub{cause: cause}}}
	if err := run.prepareValidation(); !errors.Is(err, cause) {
		t.Fatal(err)
	}
}

func TestReviewApprovalScreenshotDropsOnlyUnavailableExternalBIOS(t *testing.T) {
	path, blob, usable, bad := "bios.bin", "blob", "MATCHED", "INVALID"
	snapshot := corevalidation.Snapshot{SchemaVersion: 1, Kind: "STATIC", BIOS: []corevalidation.BIOSDependency{
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{DeliveryKind: "PROVIDER_BUNDLED"}},
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{DeliveryKind: "EXTERNAL_FILE", EmulatorPath: &path}, BlobID: &blob, InstallationStatus: &usable},
		{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{DeliveryKind: "EXTERNAL_FILE", EmulatorPath: &path}, BlobID: &blob, InstallationStatus: &bad},
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
	if err != nil || len(actual.BIOS) != 2 || actual.BIOS[0].DeliveryKind != "PROVIDER_BUNDLED" || actual.BIOS[1].BlobID == nil {
		t.Fatalf("snapshot=%+v err=%v", actual, err)
	}
}

func validApprovalDisc(ordinal int, size int64) ApprovalDisc {
	blob, name, index := "blob", "disc.chd", int64(ordinal)
	return ApprovalDisc{Ordinal: ordinal, State: "PRESENT", LogicalName: name, BlobID: &blob, SourceBlobID: &blob, SourceLogicalName: &name, SourceOrdinal: &index, SizeBytes: &size}
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
				disc.SourceBlobID = nil
			case "blob":
				disc.BlobID = nil
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
	if total, err := approvalDiscTotal([]ApprovalDisc{validApprovalDisc(0, math.MaxInt64), validApprovalDisc(1, 8)}); !errors.Is(err, ErrInvalid) || total != 0 {
		t.Fatalf("overflow total=%d err=%v", total, err)
	}
}

func TestReviewApprovalArcadeUsesDefaultBIOSAndExactRequiredROMs(t *testing.T) {
	selected, other := "selected", "other"
	requirements := ApprovalArcadeRequirements{DefaultBIOS: &selected, ROMs: []ApprovalArcadeROM{
		{Name: "main", Status: "GOOD"},
		{Name: "undumped", Status: "NODUMP"},
		{Name: "bios", Status: "GOOD", BIOSName: &selected},
		{Name: "other", Status: "GOOD", BIOSName: &other},
	}}
	if !sameApprovalRequirementNames(requirements, []string{"main", "bios"}) {
		t.Fatal("canonical ROM requirements rejected")
	}
	for _, names := range [][]string{{"bios", "main"}, {"main"}, {"main", "bios", "other"}, {"main", "bios", "undumped"}} {
		if sameApprovalRequirementNames(requirements, names) {
			t.Fatalf("noncanonical requirements=%v", names)
		}
	}
}
