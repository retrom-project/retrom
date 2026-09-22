package payloadrelease

import (
	"context"
	"errors"
	"testing"
)

type releaseGraphMemory struct {
	scheduleMemory
	links   []Scope
	linkErr error
}

func (memory *releaseGraphMemory) BeginRelease(ctx context.Context, change OwnerRelease) error {
	if err := memory.scheduleMemory.BeginRelease(ctx, change); err != nil {
		return err
	}
	owner := change.Before
	owner.PayloadState = "RELEASING"
	owner.ReleaseJobID = change.JobID
	memory.owners[owner.Scope] = owner
	return nil
}

func (memory *releaseGraphMemory) BoundSources(_ context.Context, _ string, after Scope, _ int) ([]Scope, error) {
	if after.ID != "" {
		return nil, nil
	}
	return memory.links, memory.linkErr
}

func (memory *releaseGraphMemory) RetainedSources(_ context.Context, batch SourceBatch, after string, _ int) ([]string, error) {
	if after != "" {
		return nil, nil
	}
	var ids []string
	for _, ref := range memory.links {
		if ref.Type == batch.Type {
			ids = append(ids, ref.ID)
		}
	}
	return ids, memory.linkErr
}

func reviewReleaseMemory() *releaseGraphMemory {
	item := Scope{Type: ScopeImportItem, ID: "ordinary"}
	job := Scope{Type: ScopeImportJob, ID: "import"}
	source := Scope{Type: ScopeSourceImportItem, ID: "source"}
	return &releaseGraphMemory{scheduleMemory: scheduleMemory{owners: map[Scope]Owner{
		item:   {Scope: item, State: "PUBLISHED", PayloadState: "RETAINED", Version: 3},
		job:    {Scope: job, State: "COMPLETED", PayloadState: "RETAINED", Version: 4},
		source: {Scope: source, State: "PUBLISHED", PayloadState: "RETAINED", Version: 5, PublicID: item.ID},
	}}, links: []Scope{source}}
}

func TestReviewReleaseSchedulesSharedSourceBeforeAggregate(t *testing.T) {
	t.Parallel()
	memory := reviewReleaseMemory()
	err := NewScheduler(scheduleIDs()).Review(t.Context(), ReleaseScope{Scheduling: memory, Links: memory}, ReviewRelease{
		ItemID: "ordinary", ImportID: "import", Reason: ReasonImportPublished, NowMS: 10,
	})
	if err != nil || len(memory.jobs) != 2 || len(memory.changes) != 3 {
		t.Fatalf("review release: %v %+v", err, memory)
	}
	if memory.changes[0].JobID != memory.changes[1].JobID || memory.changes[2].Before.Scope.Type != ScopeImportJob {
		t.Fatalf("shared source or aggregate order lost: %+v", memory.changes)
	}
}

func TestReviewReleasePreservesSourceReadCauseAndStopsAggregate(t *testing.T) {
	t.Parallel()
	memory := reviewReleaseMemory()
	cause := errors.New("source links unavailable")
	memory.linkErr = cause
	err := NewScheduler(scheduleIDs()).Review(t.Context(), ReleaseScope{Scheduling: memory, Links: memory}, ReviewRelease{
		ItemID: "ordinary", ImportID: "import", Reason: ReasonImportPublished, NowMS: 10,
	})
	if !errors.Is(err, cause) || len(memory.jobs) != 1 {
		t.Fatalf("source failure continued or lost cause: %v %+v", err, memory.jobs)
	}
}
