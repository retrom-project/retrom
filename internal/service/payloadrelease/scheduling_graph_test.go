package payloadrelease

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/payloadrelease"
)

type releaseGraphMemory struct {
	scheduleMemory
	links   []model.Scope
	linkErr error
}

func (memory *releaseGraphMemory) BeginRelease(ctx context.Context, change model.OwnerRelease) error {
	if err := memory.scheduleMemory.BeginRelease(ctx, change); err != nil {
		return err
	}
	owner := change.Before
	owner.PayloadState = "RELEASING"
	owner.ReleaseJobID = change.JobID
	memory.owners[owner.Scope] = owner
	return nil
}

func (memory *releaseGraphMemory) BoundSources(_ context.Context, _ string, after model.Scope, _ int) ([]model.Scope, error) {
	if after.ID != "" {
		return nil, nil
	}
	return memory.links, memory.linkErr
}

func (memory *releaseGraphMemory) RetainedSources(_ context.Context, batch model.SourceBatch, after string, _ int) ([]string, error) {
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
	item := model.Scope{Type: model.ScopeImportItem, ID: "ordinary"}
	job := model.Scope{Type: model.ScopeImportJob, ID: "import"}
	source := model.Scope{Type: model.ScopeEmulationStationImportItem, ID: "source"}
	return &releaseGraphMemory{scheduleMemory: scheduleMemory{owners: map[model.Scope]model.Owner{
		item:   {Scope: item, State: "PUBLISHED", PayloadState: "RETAINED", Version: 3},
		job:    {Scope: job, State: "COMPLETED", PayloadState: "RETAINED", Version: 4},
		source: {Scope: source, State: "PUBLISHED", PayloadState: "RETAINED", Version: 5, PublicID: item.ID},
	}}, links: []model.Scope{source}}
}

func TestReviewReleaseSchedulesSharedSourceBeforeAggregate(t *testing.T) {
	t.Parallel()
	memory := reviewReleaseMemory()
	err := NewScheduler(scheduleIDs()).Review(t.Context(), model.ReleaseScope{Scheduling: memory, Links: memory}, model.ReviewRelease{
		ItemID: "ordinary", ImportID: "import", Reason: model.ReasonImportPublished, NowMS: 10,
	})
	if err != nil || len(memory.jobs) != 2 || len(memory.changes) != 3 {
		t.Fatalf("review release: %v %+v", err, memory)
	}
	if memory.changes[0].JobID != memory.changes[1].JobID || memory.changes[2].Before.Scope.Type != model.ScopeImportJob {
		t.Fatalf("shared source or aggregate order lost: %+v", memory.changes)
	}
}

func TestReviewReleasePreservesSourceReadCauseAndStopsAggregate(t *testing.T) {
	t.Parallel()
	memory := reviewReleaseMemory()
	cause := errors.New("source links unavailable")
	memory.linkErr = cause
	err := NewScheduler(scheduleIDs()).Review(t.Context(), model.ReleaseScope{Scheduling: memory, Links: memory}, model.ReviewRelease{
		ItemID: "ordinary", ImportID: "import", Reason: model.ReasonImportPublished, NowMS: 10,
	})
	if !errors.Is(err, cause) || len(memory.jobs) != 1 {
		t.Fatalf("source failure continued or lost cause: %v %+v", err, memory.jobs)
	}
}
