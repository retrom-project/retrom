package jobs

import (
	"context"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("JOB_NOT_FOUND")

const EventBatchSize = 1000

// ReadRecords provides one consistent view of progress and its persisted events.
type ReadRecords interface {
	Detail(context.Context, string) (Snapshot, error)
	ImportProgress(context.Context, string) (ImportProgress, error)
	EventMaximum(context.Context) (int64, error)
	JobEvents(context.Context, EventQuery) ([]Event, error)
	ImportEvents(context.Context, EventQuery) ([]Event, error)
}

type Snapshot struct {
	JobID        string  `json:"jobId"`
	ScopeType    string  `json:"scopeType"`
	ScopeID      string  `json:"scopeId"`
	Kind         string  `json:"kind"`
	State        string  `json:"state"`
	Version      int64   `json:"version"`
	AttemptCount int64   `json:"attemptCount"`
	MaxAttempts  int64   `json:"maxAttempts"`
	ErrorCode    *string `json:"errorCode"`
	Retryable    bool    `json:"retryable"`
	UpdatedAtMS  int64   `json:"updatedAtMs"`
}

type ImportProgress struct {
	ImportJobID            string `json:"importJobId"`
	State                  string `json:"state"`
	Version                int64  `json:"version"`
	TotalItemCount         int64  `json:"totalItemCount"`
	QueuedItemCount        int64  `json:"queuedItemCount"`
	RunningItemCount       int64  `json:"runningItemCount"`
	ReviewPendingItemCount int64  `json:"reviewPendingItemCount"`
	FailedItemCount        int64  `json:"failedItemCount"`
}

type EventQuery struct {
	ResourceID string
	After      int64
	Limit      int
}

type Event struct {
	ID   int64
	Type string
	Data string
}

type EventBatch struct {
	Events   []Event
	Terminal bool
}

func (service *Service) Get(ctx context.Context, id string) (Snapshot, error) {
	var snapshot Snapshot
	err := service.repository.WithRead(ctx, func(records ReadRecords) error {
		var err error
		snapshot, err = records.Detail(ctx, id)
		if err != nil {
			return fmt.Errorf("read job: %w", err)
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("read job detail: %w", err)
	}
	return snapshot, nil
}

func (service *Service) JobStreamSnapshot(ctx context.Context, id string) (Snapshot, int64, error) {
	return snapshotAtWatermark(ctx, service.repository, func(records ReadRecords) (Snapshot, error) {
		snapshot, err := records.Detail(ctx, id)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read job snapshot: %w", err)
		}
		return snapshot, nil
	})
}

func (service *Service) ImportStreamSnapshot(ctx context.Context, id string) (ImportProgress, int64, error) {
	return snapshotAtWatermark(ctx, service.repository, func(records ReadRecords) (ImportProgress, error) {
		snapshot, err := records.ImportProgress(ctx, id)
		if err != nil {
			return ImportProgress{}, fmt.Errorf("read import snapshot: %w", err)
		}
		return snapshot, nil
	})
}

func snapshotAtWatermark[T any](ctx context.Context, repository Repository,
	read func(ReadRecords) (T, error),
) (T, int64, error) {
	var snapshot T
	var maximum int64
	err := repository.WithRead(ctx, func(records ReadRecords) error {
		var err error
		snapshot, err = read(records)
		if err != nil {
			return err
		}
		maximum, err = records.EventMaximum(ctx)
		if err != nil {
			return fmt.Errorf("read event maximum: %w", err)
		}
		return nil
	})
	if err != nil {
		var zero T
		return zero, 0, fmt.Errorf("read stream snapshot: %w", err)
	}
	return snapshot, maximum, nil
}

func (service *Service) JobEvents(ctx context.Context, id string, after int64) (EventBatch, error) {
	return service.readEventBatch(ctx, func(records ReadRecords) (EventBatch, error) {
		snapshot, err := records.Detail(ctx, id)
		if err != nil {
			return EventBatch{}, fmt.Errorf("read job state: %w", err)
		}
		events, err := records.JobEvents(ctx, EventQuery{ResourceID: id, After: after, Limit: EventBatchSize})
		return progressBatch(events, jobTerminal(snapshot.State), err)
	})
}

func (service *Service) ImportEvents(ctx context.Context, id string, after int64) (EventBatch, error) {
	return service.readEventBatch(ctx, func(records ReadRecords) (EventBatch, error) {
		snapshot, err := records.ImportProgress(ctx, id)
		if err != nil {
			return EventBatch{}, fmt.Errorf("read import state: %w", err)
		}
		events, err := records.ImportEvents(ctx, EventQuery{ResourceID: id, After: after, Limit: EventBatchSize})
		return progressBatch(events, importTerminal(snapshot.State), err)
	})
}

func progressBatch(events []Event, terminal bool, err error) (EventBatch, error) {
	if err != nil {
		return EventBatch{}, fmt.Errorf("read progress events: %w", err)
	}
	return EventBatch{Events: events, Terminal: terminal && len(events) < EventBatchSize}, nil
}

func (service *Service) readEventBatch(ctx context.Context,
	read func(ReadRecords) (EventBatch, error),
) (EventBatch, error) {
	var batch EventBatch
	err := service.repository.WithRead(ctx, func(records ReadRecords) error {
		var err error
		batch, err = read(records)
		return err
	})
	if err != nil {
		return EventBatch{}, fmt.Errorf("read event batch snapshot: %w", err)
	}
	return batch, nil
}

func jobTerminal(state string) bool {
	return state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED"
}

func importTerminal(state string) bool {
	return state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" ||
		state == "REVIEW_PENDING" || state == "PARTIAL_FAILURE"
}
