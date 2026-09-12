package serverimport

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"retrom/internal/firmware"
	"retrom/internal/serversource"
)

type discoveryMemory struct {
	plan    DiscoveryPlan
	writes  int
	lateErr error
}

func (memory *discoveryMemory) WithWrite(_ context.Context, work func(DiscoveryRecords) error) error {
	if err := work(memory); err != nil {
		return err
	}
	return memory.lateErr
}

func (memory *discoveryMemory) Reset(context.Context, Work, int64) error { memory.writes++; return nil }

func (memory *discoveryMemory) Persist(_ context.Context, plan DiscoveryPlan) error {
	memory.plan = plan
	memory.writes++
	return nil
}

func TestDiscoveryFreezesRanksAndDuplicateReasons(t *testing.T) {
	item := CatalogItem{RequirementID: "item", SourceKind: "STATIC", LogicalName: "bios.bin"}
	exact := &EvaluatedCandidate{ID: "exact", Item: item, File: serversource.File{Basename: "bios.bin"}, State: "ELIGIBLE", Static: &firmware.StaticEvaluation{ExactHash: true}}
	fallback := &EvaluatedCandidate{ID: "fallback", Item: item, State: "ELIGIBLE", Static: &firmware.StaticEvaluation{ExpectedSizeMatched: true}}
	duplicate := &EvaluatedCandidate{ID: "duplicate", Item: item, State: "DUPLICATE_BYTES"}
	memory := &discoveryMemory{}
	groups := map[string][]*EvaluatedCandidate{"item": {fallback, exact, duplicate}}
	err := NewDiscovery(memory, time.Now).Persist(t.Context(), Work{}, groups, serversource.Counts{SkippedSpecial: 2})
	if err != nil {
		t.Fatal(err)
	}
	values := memory.plan.Groups[0].Candidates
	if memory.plan.Total != 3 || memory.plan.Multiple != 1 || memory.plan.Counts.SkippedSpecial != 2 || *values[0].Rank != 2 || *values[0].NotSelected != "LOWER_RANK" || *values[1].Rank != 1 || values[1].NotSelected != nil || !values[1].ExactBasename || values[2].Rank != nil || *values[2].NotSelected != "DUPLICATE_BYTES" {
		t.Fatalf("discovery plan: %+v", memory.plan)
	}
	if groups["item"][0] != fallback {
		t.Fatal("ranking changed discovery order")
	}
}

func TestDiscoveryEncodesEvidenceBeforeOpeningWriteTransaction(t *testing.T) {
	memory := &discoveryMemory{}
	candidate := &EvaluatedCandidate{Item: CatalogItem{RequirementID: "item"}, State: "INELIGIBLE", Details: map[string]any{"invalid": math.NaN()}}
	err := NewDiscovery(memory, time.Now).Persist(t.Context(), Work{}, map[string][]*EvaluatedCandidate{"item": {candidate}}, serversource.Counts{})
	if err == nil || memory.writes != 0 {
		t.Fatalf("invalid evidence reached storage: %v writes=%d", err, memory.writes)
	}
	memory.lateErr = context.Canceled
	if err := NewDiscovery(memory, time.Now).Reset(t.Context(), Work{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("reset commit failure: %v", err)
	}
}
