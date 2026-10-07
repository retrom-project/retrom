package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func TestReadinessRequiresAdminBeforeReading(t *testing.T) {
	s := &Service{}
	_, err := s.Readiness(context.Background(), model.Principal{User: model.User{Role: "user"}}, []string{"abc"})
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
		{Requirements: []runtimeclient.BiosRequirement{
			missing,
			{RequirementKey: "installed", Required: true},
			{RequirementKey: "optional", Required: false},
			{RequirementKey: "unconstrained", LogicalName: "firmware.zip", CoreID: "arcade", Required: true},
		}},
		{Requirements: []runtimeclient.BiosRequirement{missing}, Error: &failure},
		{Requirements: []runtimeclient.BiosRequirement{missing}},
	}, []model.BiosFile{{RequirementKey: "installed"}})
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
	applyBIOSProjections(items, []int{2}, []biosProjection{{Requirements: []runtimeclient.BiosRequirement{missing}}},
		[]model.BiosFile{{RequirementKey: missing.RequirementKey}})
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
		{Requirements: []runtimeclient.BiosRequirement{}},
		{Requirements: []runtimeclient.BiosRequirement{{RequirementKey: "optional", Required: false}}},
		{Requirements: []runtimeclient.BiosRequirement{{RequirementKey: "installed", Required: true}}},
		{Requirements: []runtimeclient.BiosRequirement{{RequirementKey: "missing", Required: true}}},
		{Requirements: []runtimeclient.BiosRequirement{}, Error: &failure},
		{},
	}
	items := make([]model.ReviewReadiness, len(projections))
	applyBIOSProjections(items, []int{0, 1, 2, 3, 4, 5}, projections, []model.BiosFile{{RequirementKey: "installed"}})
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
	applyBIOSProjections(items, []int{0, 1}, []biosProjection{{Requirements: []runtimeclient.BiosRequirement{}}}, nil)
	for _, item := range items {
		if item.BIOSSatisfied != nil || item.Error == nil {
			t.Fatalf("incomplete batch became eligible: %#v", item)
		}
	}
}
