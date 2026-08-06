package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/cleanup"
)

var (
	ErrConflict       = errors.New("JOB_CONFLICT")
	ErrRetryViaDomain = errors.New("RETRY_VIA_DOMAIN_ACTION")
)

type Result struct {
	JobID       string `json:"jobId"`
	State       string `json:"state"`
	ExecutionNo int64  `json:"executionNo"`
	Version     int64  `json:"version"`
}

type Service struct {
	database *sql.DB
	now      func() time.Time
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func validReason(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len([]rune(value)) <= 500
}

func (service *Service) Cancel(
	ctx context.Context,
	jobID string,
	expectedVersion int64,
	reason string,
) (Result, bool, error) {
	if !validReason(reason) {
		return Result{}, false, ErrConflict
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, false, fmt.Errorf("jobs/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var cancellable, executionNo, version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
cancellable,
execution_no,
version
FROM jobs
WHERE id=?
`, jobID).Scan(&state, &cancellable, &executionNo, &version); err != nil ||
		version != expectedVersion ||
		cancellable != 1 ||
		state != "QUEUED" && state != "RUNNING" {
		return Result{}, false, ErrConflict
	}
	now := service.now().UnixMilli()
	pending := state == "RUNNING"
	newState := "CANCELLED"
	var finished any = now
	if pending {
		newState, finished = "CANCEL_REQUESTED", nil
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=?,
cancel_requested_at_ms=?,
cancel_reason=?,
finished_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?;
 INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
?,
json_object('reason',
?),
?
FROM jobs
WHERE id=?
`,
		newState,
		now,
		strings.TrimSpace(reason),
		finished,
		now,
		jobID,
		expectedVersion,
		newState,
		strings.TrimSpace(reason),
		now,
		jobID,
	); err != nil {
		return Result{}, false, fmt.Errorf("jobs/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Result{}, false, fmt.Errorf("jobs/service: %w", err)
	}
	return Result{JobID: jobID, State: newState, ExecutionNo: executionNo, Version: version + 1}, pending, nil
}

//nolint:funlen // Retry eligibility, attempt creation, event emission, and optimistic locking share one transaction.
func (service *Service) Retry(ctx context.Context, jobID string, expectedVersion int64) (Result, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("jobs/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var kind, state, payload string
	var retryable sql.NullInt64
	var executionNo, version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT kind,
state,
error_retryable,
payload_json,
execution_no,
version
FROM jobs
WHERE id=?
`, jobID).Scan(&kind, &state, &retryable, &payload, &executionNo, &version); err != nil ||
		version != expectedVersion ||
		state != "FAILED" ||
		!retryable.Valid ||
		retryable.Int64 != 1 {
		return Result{}, ErrConflict
	}
	if kind == "METADATA_SCRAPE" {
		return Result{}, ErrRetryViaDomain
	}
	executionNo++
	now := service.now().UnixMilli()
	digest := sha256.Sum256([]byte(payload))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
?,
?,
?,
?);
 UPDATE jobs
SET state='QUEUED',
execution_no=?,
attempt_count=0,
available_at_ms=?,
execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,
leased_until_ms=NULL,
heartbeat_at_ms=NULL,
finished_at_ms=NULL,
worker_id=NULL,
error_code=NULL,
error_retryable=NULL,
cancel_requested_at_ms=NULL,
cancel_reason=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?;
 INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'MANUAL_RETRY',
json_object('executionNo',
?),
?
FROM jobs
WHERE id=?
`,
		jobID,
		executionNo,
		payload,
		hex.EncodeToString(digest[:]),
		now,
		executionNo,
		now,
		now,
		jobID,
		expectedVersion,
		executionNo,
		now,
		jobID,
	); err != nil {
		return Result{}, fmt.Errorf("jobs/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Result{}, fmt.Errorf("jobs/service: %w", err)
	}
	return Result{JobID: jobID, State: "QUEUED", ExecutionNo: executionNo, Version: version + 1}, nil
}
