package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	model "retrom/internal/model/jobs"
)

var ErrRetryViaDomain = errors.New("RETRY_VIA_DOMAIN_ACTION")

type Result struct {
	Kind        string `json:"-"`
	JobID       string `json:"jobId"`
	State       string `json:"state"`
	ExecutionNo int64  `json:"executionNo"`
	Version     int64  `json:"version"`
}

type Service struct {
	repository    model.Repository
	cancellations map[string]DomainCanceller
	now           func() time.Time
}

func New(repository model.Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func validReason(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len([]rune(value)) <= 500
}

func cancellableJobState(state string, retryable bool) bool {
	return state == "QUEUED" || state == "RUNNING" || state == "FAILED" && retryable
}

func (service *Service) Cancel(
	ctx context.Context, jobID string, expectedVersion int64, reason string,
) (Result, bool, error) {
	if !validReason(reason) {
		return Result{}, false, model.ErrConflict
	}
	var result Result
	var dispatch domainDispatch
	pending := false
	err := service.repository.WithWrite(ctx, func(records model.Records) error {
		job, err := records.Get(ctx, jobID)
		if err != nil {
			return fmt.Errorf("read cancellation job: %w", err)
		}
		if job.Version != expectedVersion || !job.Cancellable || !cancellableJobState(job.State, job.Retryable) {
			return model.ErrConflict
		}
		if job.Kind == "REVIEW_BULK_APPROVE" {
			return ErrRetryViaDomain
		}
		if handler := service.cancellations[job.Kind]; handler != nil {
			dispatch = domainDispatch{handler: handler, command: DomainCancellation{
				JobID: jobID, Kind: job.Kind, ScopeID: job.ScopeID,
				Reason: strings.TrimSpace(reason), ExpectedVersion: expectedVersion,
			}}
			return nil
		}
		now := service.now().UnixMilli()
		change := model.Cancellation{
			JobID: jobID, ExpectedVersion: expectedVersion, State: "CANCELLED",
			Reason: strings.TrimSpace(reason), AtMS: now, FinishedAtMS: &now,
		}
		pending = job.State == "RUNNING"
		if pending {
			change.State, change.FinishedAtMS = "CANCEL_REQUESTED", nil
		}
		change.Event, err = json.Marshal(struct {
			Reason string `json:"reason"`
		}{Reason: change.Reason})
		if err != nil {
			return fmt.Errorf("encode cancellation event: %w", err)
		}
		if err := records.Cancel(ctx, change); err != nil {
			return fmt.Errorf("cancel job: %w", err)
		}
		if job.Kind == "SERVER_BIOS_IMPORT" {
			if err := records.CancelServerImport(ctx, change); err != nil {
				return fmt.Errorf("cancel server import: %w", err)
			}
		}
		result = Result{JobID: jobID, State: change.State, ExecutionNo: job.ExecutionNo, Version: job.Version + 1}
		return nil
	})
	if err != nil {
		return Result{}, false, fmt.Errorf("jobs/cancel: %w", err)
	}
	if dispatch.handler != nil {
		return dispatch.cancel(ctx)
	}
	return result, pending, nil
}
