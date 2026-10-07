package library

import (
	"context"
	"errors"
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
