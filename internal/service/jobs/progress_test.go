package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/jobs"
)

func TestProgressBatchDrainsTerminalBacklog(t *testing.T) {
	for _, test := range []struct {
		name, state string
		count       int
		importScope bool
		terminal    bool
	}{
		{"running empty", "RUNNING", 0, false, false},
		{"success full page", "SUCCEEDED", 1000, false, false},
		{"success last page", "SUCCEEDED", 1, false, true},
		{"cancelled empty", "CANCELLED", 0, false, true},
		{"failed empty", "FAILED", 0, false, true},
		{"pending import", "CANCEL_REQUESTED", 0, true, false},
		{"partial import", "PARTIAL_FAILURE", 1, true, true},
		{"review backlog", "REVIEW_PENDING", 1000, true, false},
		{"review caught up", "REVIEW_PENDING", 0, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &progressRepository{state: test.state, events: make([]model.Event, test.count)}
			service := New(repository, time.Now)
			read := service.JobEvents
			if test.importScope {
				read = service.ImportEvents
			}
			batch, err := read(t.Context(), "target", 41)
			if err != nil || batch.Terminal != test.terminal || len(batch.Events) != test.count {
				t.Fatalf("batch terminal=%v count=%d error=%v", batch.Terminal, len(batch.Events), err)
			}
			if repository.opened != 1 || repository.query != (model.EventQuery{ResourceID: "target", After: 41, Limit: 1000}) {
				t.Fatalf("batch crossed read scope or changed cursor: %+v", repository)
			}
		})
	}
}

func TestProgressSnapshotsShareReadScope(t *testing.T) {
	for _, importScope := range []bool{false, true} {
		repository := &progressRepository{state: "RUNNING"}
		service := New(repository, time.Now)
		var maximum int64
		var err error
		if importScope {
			_, maximum, err = service.ImportStreamSnapshot(t.Context(), "target")
		} else {
			_, maximum, err = service.JobStreamSnapshot(t.Context(), "target")
		}
		if err != nil || maximum != 42 || repository.opened != 1 || repository.maximumReads != 1 {
			t.Fatalf("snapshot outside scope: maximum=%d repository=%+v error=%v", maximum, repository, err)
		}
	}
}

func TestProgressReadFailurePreservesCause(t *testing.T) {
	failure := errors.New("snapshot unavailable")
	for _, stage := range []string{"detail", "events", "maximum"} {
		repository := &progressRepository{failure: failure, failAt: stage}
		service := New(repository, time.Now)
		var err error
		if stage == "maximum" {
			_, _, err = service.JobStreamSnapshot(t.Context(), "job")
		} else {
			_, err = service.JobEvents(t.Context(), "job", 0)
		}
		if !errors.Is(err, failure) {
			t.Fatalf("%s lost cause: %v", stage, err)
		}
	}
}

type progressRepository struct {
	state                string
	events               []model.Event
	opened, maximumReads int
	query                model.EventQuery
	failAt               string
	failure              error
}

func (r *progressRepository) LoadDetail(_ context.Context, _ string) (model.Snapshot, error) {
	r.opened++
	if r.failAt == "detail" {
		return model.Snapshot{}, r.failure
	}
	return model.Snapshot{State: r.state}, nil
}

func (r *progressRepository) LoadJobStreamSnapshot(_ context.Context, _ string) (model.Snapshot, int64, error) {
	r.opened++
	r.maximumReads++
	if r.failAt == "maximum" {
		return model.Snapshot{}, 0, r.failure
	}
	return model.Snapshot{State: r.state}, 42, nil
}

func (r *progressRepository) LoadImportStreamSnapshot(_ context.Context, _ string) (model.ImportProgress, int64, error) {
	r.opened++
	r.maximumReads++
	return model.ImportProgress{State: r.state}, 42, nil
}

func (r *progressRepository) LoadJobEvents(_ context.Context, id string, after int64) ([]model.Event, model.Snapshot, error) {
	r.opened++
	r.query = model.EventQuery{ResourceID: id, After: after, Limit: 1000}
	if r.failAt == "detail" {
		return nil, model.Snapshot{}, r.failure
	}
	if r.failAt == "events" {
		return nil, model.Snapshot{}, r.failure
	}
	return r.events, model.Snapshot{State: r.state}, nil
}

func (r *progressRepository) LoadImportEvents(_ context.Context, id string, after int64) ([]model.Event, model.ImportProgress, error) {
	r.opened++
	r.query = model.EventQuery{ResourceID: id, After: after, Limit: 1000}
	return r.events, model.ImportProgress{State: r.state}, nil
}

func (r *progressRepository) CommitCancel(context.Context, model.CancelCommand) (model.CancelResult, error) {
	return model.CancelResult{}, nil
}

func (r *progressRepository) CommitRetry(context.Context, model.RetryCommand) (model.Result, error) {
	return model.Result{}, nil
}
