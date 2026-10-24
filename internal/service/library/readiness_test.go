package library

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func TestReadinessRequiresAdminBeforeReading(t *testing.T) {
	s := &Service{}
	_, err := s.Readiness(context.Background(), model.Principal{User: model.User{Role: "user"}}, []string{"abc"}, "")
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("non-admin result = %v", err)
	}
}

func TestReadinessPreservesOnlyMissingRequiredFileMetadata(t *testing.T) {
	size := int64(16384)
	sha, md5 := "runtime-sha256", "runtime-md5"
	missing := runtimeclient.BiosRequirement{
		RequirementKey: "provider/target/os.rom", LogicalName: "os.rom", CoreID: "core", Required: true,
		SizeBytes: &size, SHA256: &sha, MD5: &md5, VirtualPath: &sha,
	}
	failure := "RUNTIME_CONTENT_UNAVAILABLE"
	items := make([]model.ReviewReadiness, 3)
	applyBIOSProjections(items, []int{0, 1, 2}, []biosProjection{
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{
			missing,
			{RequirementKey: "installed", Required: true},
			{RequirementKey: "optional", Required: false},
			{RequirementKey: "unconstrained", LogicalName: "firmware.zip", CoreID: "arcade", Required: true},
		}},
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{missing}, Error: &failure},
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{missing}},
	}, []model.BiosFile{{RequirementKey: "installed"}}, nil)
	want := []model.ReviewMissingBIOS{
		{Key: missing.RequirementKey, Name: "os.rom", CoreID: "core"},
		{Key: "unconstrained", Name: "firmware.zip", CoreID: "arcade"},
	}
	if !reflect.DeepEqual(items[0].MissingBIOS, want) {
		t.Fatalf("missing metadata = %#v", items[0].MissingBIOS)
	}
	if items[1].BIOSSatisfied != nil || items[1].Error == nil || len(items[1].MissingBIOS) != 0 {
		t.Fatalf("inspection error exposed misleading missing files: %#v", items[1])
	}
	applyBIOSProjections(items, []int{2}, []biosProjection{{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{missing}}},
		[]model.BiosFile{{RequirementKey: missing.RequirementKey}}, nil)
	if items[2].BIOSSatisfied == nil || !*items[2].BIOSSatisfied || len(items[2].MissingBIOS) != 0 {
		t.Fatalf("new installation retained stale missing files: %#v", items[2])
	}
}

func TestReadinessIDsBoundedAndUnique(t *testing.T) {
	id := "01a11261-a55c-7e8a-87bf-e0dd5934314c"
	for name, ids := range map[string][]string{"empty": {}, "invalid": {"wrong"}, "duplicate": {id, id}, "tooMany": make([]string, 101)} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(readinessIDs(ids), model.ErrInvalid) {
				t.Fatal("expected invalid")
			}
		})
	}
	if err := readinessIDs([]string{id}); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessUsesRuntimeRequirementsWithoutConflatingUnknownAndMissing(t *testing.T) {
	failure := "RUNTIME_UNKNOWN"
	projections := []biosProjection{
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{}},
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{{RequirementKey: "optional", Required: false}}},
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{{RequirementKey: "installed", Required: true}}},
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{{RequirementKey: "missing", Required: true}}},
		{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{}, Error: &failure},
		{},
	}
	items := make([]model.ReviewReadiness, len(projections))
	applyBIOSProjections(items, []int{0, 1, 2, 3, 4, 5}, projections, []model.BiosFile{{RequirementKey: "installed"}}, nil)
	for index := range 4 {
		if items[index].Error != nil || items[index].BIOSSatisfied == nil || *items[index].BIOSSatisfied != (index < 3) {
			t.Fatalf("item%d = %#v", index, items[index])
		}
	}
	for _, index := range []int{4, 5} {
		if items[index].BIOSSatisfied != nil || items[index].Error == nil {
			t.Fatalf("unknown item%d = %#v", index, items[index])
		}
	}
}

func TestReadinessRejectsIncompleteRuntimeBatch(t *testing.T) {
	items := make([]model.ReviewReadiness, 2)
	applyBIOSProjections(items, []int{0, 1}, []biosProjection{{MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{}, Requirements: []runtimeclient.BiosRequirement{}}}, nil, nil)
	for _, item := range items {
		if item.BIOSSatisfied != nil || item.Error == nil {
			t.Fatalf("incomplete batch became eligible: %#v", item)
		}
	}
}

func TestReadinessKeepsCRCWarningsInformational(t *testing.T) {
	warning := model.ReviewCRCWarning{Role: "BIOS", File: "game.zip", Member: "bios.bin", ExpectedCRC32: "12345678", ActualCRC32: "ffffffff"}
	items := make([]model.ReviewReadiness, 1)
	applyBIOSProjections(items, []int{0}, []biosProjection{{Requirements: []runtimeclient.BiosRequirement{}, MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{warning}}}, nil, nil)
	if items[0].Error != nil || items[0].BIOSSatisfied == nil || !*items[0].BIOSSatisfied || !reflect.DeepEqual(items[0].CRCWarnings, []model.ReviewCRCWarning{warning}) {
		t.Fatalf("CRC-only mismatch blocked BIOS readiness: %#v", items[0])
	}
}

func TestReadinessPreservesActionableRuntimeFailures(t *testing.T) {
	for _, code := range []string{
		"RUNTIME_ARCADE_MACHINE_UNKNOWN", "RUNTIME_FLYCAST_MACHINE_UNKNOWN",
		"RUNTIME_FLYCAST_MEDIA_UNSUPPORTED", "RUNTIME_FLYCAST_ROM_MISSING",
		"RUNTIME_PARENT_MISSING", "RUNTIME_ENTRY_MISSING", "RUNTIME_CONTENT_UNAVAILABLE",
	} {
		t.Run(code, func(t *testing.T) {
			items := make([]model.ReviewReadiness, 1)
			applyBIOSProjections(items, []int{0}, []biosProjection{{Error: &code}}, nil, nil)
			item := items[0]
			if item.Error == nil || item.Error.Code != code || item.Error.Message == "" ||
				strings.Contains(item.Error.Message, "BIOS") || item.BIOSSatisfied != nil || len(item.MissingBIOS) != 0 {
				t.Fatalf("runtime failure became a BIOS failure or eligible item: %#v", item)
			}
		})
	}
}

func TestReadinessUnknownFailureDoesNotExposeRuntimeText(t *testing.T) {
	failure := "private/path: arbitrary runtime exception"
	items := make([]model.ReviewReadiness, 1)
	applyBIOSProjections(items, []int{0}, []biosProjection{{Error: &failure}}, nil, nil)
	if items[0].Error == nil || items[0].Error.Code != "REVIEW_READINESS_UNAVAILABLE" ||
		strings.Contains(items[0].Error.Message, failure) || items[0].BIOSSatisfied != nil {
		t.Fatalf("unsafe or misleading fallback: %#v", items[0])
	}
}

func TestReadinessPreservesDATGapsIndependentlyOfBIOSAndCRC(t *testing.T) {
	missing := model.ReviewMissingROMMember{CoreID: "mame_arcade", ROMSet: "sample", Role: "ROM", Member: "eeprom.bin", SizeBytes: 548, Reason: "MISSING"}
	items := make([]model.ReviewReadiness, 1)
	projection := biosProjection{Requirements: []runtimeclient.BiosRequirement{}, CRCWarnings: []model.ReviewCRCWarning{}, MissingROMMembers: []model.ReviewMissingROMMember{missing}}
	applyBIOSProjections(items, []int{0}, []biosProjection{projection}, nil, nil)
	if items[0].Error != nil || items[0].BIOSSatisfied == nil || !*items[0].BIOSSatisfied || !reflect.DeepEqual(items[0].MissingROMMembers, []model.ReviewMissingROMMember{missing}) {
		t.Fatalf("DAT gap lost or mislabeled as BIOS: %+v", items[0])
	}
	projection.MissingROMMembers = nil
	applyBIOSProjections(items, []int{0}, []biosProjection{projection}, nil, nil)
	if items[0].Error == nil || items[0].BIOSSatisfied != nil || len(items[0].MissingROMMembers) != 0 {
		t.Fatalf("missing runtime facts treated as complete: %+v", items[0])
	}
}

func TestReadinessTreatsAnUnusableInstalledArchiveAsMissing(t *testing.T) {
	projection := biosProjection{
		MissingROMMembers: []model.ReviewMissingROMMember{}, CRCWarnings: []model.ReviewCRCWarning{},
		Requirements: []runtimeclient.BiosRequirement{{
			RequirementKey: "retrom-runtime/rpgmaker-xp/Standard.zip", LogicalName: "Standard.zip", CoreID: "rpgmaker",
			Required: true, ArchiveRoot: []string{"Audio/", "Graphics/"},
		}},
	}
	installed := []model.BiosFile{{RequirementKey: "retrom-runtime/rpgmaker-xp/Standard.zip"}}
	for _, unusable := range []bool{false, true} {
		items := make([]model.ReviewReadiness, 1)
		applyBIOSProjections(items, []int{0}, []biosProjection{projection}, installed,
			map[string]bool{"retrom-runtime/rpgmaker-xp/Standard.zip": unusable})
		if items[0].BIOSSatisfied == nil || *items[0].BIOSSatisfied == unusable || len(items[0].MissingBIOS) != map[bool]int{false: 0, true: 1}[unusable] {
			t.Fatalf("unusable=%v item=%#v", unusable, items[0])
		}
	}
}
