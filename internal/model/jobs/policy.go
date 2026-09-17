package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// CancellableJobState returns true if a job in this state can be cancelled.
func CancellableJobState(state string, retryable bool) bool {
	return state == "QUEUED" || state == "RUNNING" || state == "FAILED" && retryable
}

// RetryEligibility checks whether a job is eligible for retry.
func RetryEligibility(job Job, expectedVersion int64) error {
	if job.Version != expectedVersion || job.State != "FAILED" || !job.Retryable {
		return ErrConflict
	}
	if job.Kind == "METADATA_SCRAPE" || job.Kind == "SERVER_BIOS_IMPORT" || job.Kind == "REVIEW_BULK_APPROVE" {
		return ErrRetryViaDomain
	}
	return nil
}

// RetryInputSnapshot prepares a retry input from the previous execution.
func RetryInputSnapshot(previous []byte, job Job) ([]byte, error) {
	var input retryInputEnvelope
	if json.Unmarshal(previous, &input) != nil || input.SchemaVersion != 1 ||
		input.Kind != job.Kind || input.Scope.Type != job.ScopeType || input.Scope.ID != job.ScopeID ||
		len(input.Inputs) == 0 || !json.Valid(input.Inputs) {
		return nil, ErrConflict
	}
	if _, err := uuid.Parse(input.ExecutionID); err != nil {
		return nil, ErrConflict
	}
	executionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("jobs/retry execution ID: %w", err)
	}
	input.ExecutionID = executionID.String()
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("jobs/retry input: %w", err)
	}
	return encoded, nil
}

// BuildRetryWrite constructs the full retry write payload.
func BuildRetryWrite(
	jobID string, expectedVersion int64, previous []byte, job Job, nowMS int64,
) (RetryWrite, Result, error) {
	input, err := RetryInputSnapshot(previous, job)
	if err != nil {
		return RetryWrite{}, Result{}, err
	}
	digest := sha256.Sum256(input)
	executionNo := job.ExecutionNo + 1
	change := RetryWrite{
		JobID: jobID, ExpectedVersion: expectedVersion,
		ExecutionNo: executionNo, AtMS: nowMS,
		Input: input, InputDigest: hex.EncodeToString(digest[:]),
		Payload: []byte(fmt.Sprintf(
			`{"schemaVersion":1,"inputExecutionNo":%d}`, executionNo,
		)),
		Event: []byte(fmt.Sprintf(`{"executionNo":%d}`, executionNo)),
	}
	result := Result{
		Kind: job.Kind, JobID: jobID,
		State: "QUEUED", ExecutionNo: executionNo,
		Version: job.Version + 1,
	}
	return change, result, nil
}

// NeedsDomainCancel reports whether this job kind's cancellation
// should be handled by a domain-specific handler rather than
// the generic cancel path.
func NeedsDomainCancel(_ string, hasDomainHandler bool) bool {
	return hasDomainHandler
}

type retryInputEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Kind          string          `json:"kind"`
	Scope         retryInputScope `json:"scope"`
	ExecutionID   string          `json:"executionId"`
	Inputs        json.RawMessage `json:"inputs"`
}

type retryInputScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
