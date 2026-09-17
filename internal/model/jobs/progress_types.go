package jobs

import (
	"context"
	"errors"
)

var (
	ErrConflict        = errors.New("JOB_CONFLICT")
	ErrNotFound        = errors.New("JOB_NOT_FOUND")
	ErrRetryViaDomain  = errors.New("RETRY_VIA_DOMAIN_ACTION")
)

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
