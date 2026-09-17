package jobs

import (
	"context"
	"errors"
	model "retrom/internal/model/jobs"
	"testing"
	"time"
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
	for _, stage := range []string{"begin", "detail", "events", "maximum"} {
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
	model.Repository
	model.ReadRecords
	state                string
	events               []model.Event
	opened, maximumReads int
	query                model.EventQuery
	failAt               string
	failure              error
}

func (repository *progressRepository) WithRead(_ context.Context, work func(model.ReadRecords) error) error {
	repository.opened++
	if repository.failAt == "begin" {
		return repository.failure
	}
	return work(repository)
}

func (repository *progressRepository) Detail(context.Context, string) (model.Snapshot, error) {
	if repository.failAt == "detail" {
		return model.Snapshot{}, repository.failure
	}
	return model.Snapshot{State: repository.state}, nil
}

func (repository *progressRepository) ImportProgress(context.Context, string) (model.ImportProgress, error) {
	return model.ImportProgress{State: repository.state}, nil
}

func (repository *progressRepository) EventMaximum(context.Context) (int64, error) {
	repository.maximumReads++
	if repository.failAt == "maximum" {
		return 0, repository.failure
	}
	return 42, nil
}

func (repository *progressRepository) JobEvents(_ context.Context, query model.EventQuery) ([]model.Event, error) {
	repository.query = query
	if repository.failAt == "events" {
		return nil, repository.failure
	}
	return repository.events, nil
}

func (repository *progressRepository) ImportEvents(ctx context.Context, query model.EventQuery) ([]model.Event, error) {
	return repository.JobEvents(ctx, query)
}
