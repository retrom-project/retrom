package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"retrom/internal/service/idempotency"

	"retrom/internal/jobinput"
)

func retryEligibility(job Job, expectedVersion int64) error {
	if job.Version != expectedVersion || job.State != "FAILED" || !job.Retryable {
		return ErrConflict
	}
	if job.Kind == "METADATA_SCRAPE" || job.Kind == "SERVER_BIOS_IMPORT" || job.Kind == "REVIEW_BULK_APPROVE" {
		return ErrRetryViaDomain
	}
	return nil
}

func (service *Service) Retry(ctx context.Context, jobID string, expectedVersion int64) (Result, error) {
	var result Result
	err := service.repository.WithWrite(ctx, func(records Records) error {
		job, err := records.Get(ctx, jobID)
		if err != nil {
			return fmt.Errorf("read retry job: %w", err)
		}
		if err := retryEligibility(job, expectedVersion); err != nil {
			return err
		}
		previous, err := records.Input(ctx, jobID, job.ExecutionNo)
		if err != nil {
			return fmt.Errorf("read retry input: %w", err)
		}
		input, err := retryInputSnapshot(previous, job)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(input)
		executionNo := job.ExecutionNo + 1
		change := RetryWrite{
			JobID: jobID, ExpectedVersion: expectedVersion, ExecutionNo: executionNo,
			AtMS: service.now().UnixMilli(), Input: input, InputDigest: hex.EncodeToString(digest[:]),
			Payload: []byte(fmt.Sprintf(`{"schemaVersion":1,"inputExecutionNo":%d}`, executionNo)),
			Event:   []byte(fmt.Sprintf(`{"executionNo":%d}`, executionNo)),
		}
		if err := records.Retry(ctx, change); err != nil {
			return fmt.Errorf("retry job: %w", err)
		}
		result = Result{Kind: job.Kind, JobID: jobID, State: "QUEUED", ExecutionNo: executionNo, Version: job.Version + 1}
		return idempotency.Complete(ctx, idempotency.Result{Value: result, Version: result.Version})
	})
	if err != nil {
		return Result{}, fmt.Errorf("jobs/retry: %w", err)
	}
	return result, nil
}

func retryInputSnapshot(previous []byte, job Job) ([]byte, error) {
	input, err := jobinput.Retry(previous, job.Kind, jobinput.Scope{Type: job.ScopeType, ID: job.ScopeID})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return input, nil
}
