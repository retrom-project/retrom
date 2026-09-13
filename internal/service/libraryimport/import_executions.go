package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/service/payloadrelease"
)

type ImportExecutions struct {
	repository ImportExecutionRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewImportExecutions(repository ImportExecutionRepository, now func() time.Time) *ImportExecutions {
	if now == nil {
		now = time.Now
	}
	return &ImportExecutions{repository: repository, now: now, newID: newImportAdmissionID}
}

func (service *ImportExecutions) Claim(ctx context.Context, id string) (ImportWork, bool, error) {
	workerID, err := service.newID()
	if err != nil {
		return ImportWork{}, false, fmt.Errorf("allocate import worker: %w", err)
	}
	var result ImportWork
	var found bool
	var problem error
	err = service.repository.WithExecution(
		ctx,
		func(scope ImportExecutionScope) error {
			before, exists, err := scope.Records.Current(ctx, id)
			if err != nil {
				return fmt.Errorf("read import claim: %w", err)
			}
			now := service.now().UnixMilli()
			if !exists || !importClaimAvailable(before, now) {
				return nil
			}
			if cause := importClaimProblem(before, now); cause != nil {
				problem = cause
				return service.rejectQueued(ctx, scope, before, cause, now)
			}
			work, err := decodeImportWork(before)
			if err == nil {
				err = validateClaimUpload(ctx, scope.Facts, before)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalid) {
					return fmt.Errorf("validate import claim: %w", err)
				}
				problem = err
				return service.rejectQueued(ctx, scope, before, err, now)
			}
			work, change, err := importClaimProjection(before, work, workerID, now)
			if err != nil {
				return err
			}
			if err := scope.Records.Transition(ctx, change); err != nil {
				return fmt.Errorf("claim import execution: %w", err)
			}
			finished := service.now().UnixMilli()
			if finished >= *change.Job.LeaseUntilMS || finished >= work.Execution.DeadlineMS {
				return ErrVersionConflict
			}
			result, found = work, true
			return nil
		},
	)
	if err != nil {
		return ImportWork{}, false, fmt.Errorf("claim import worker: %w", err)
	}
	if problem != nil {
		return ImportWork{}, false, fmt.Errorf("reject import work: %w", problem)
	}
	return result, found, nil
}

func importClaimAvailable(before ImportWorkerSnapshot, now int64) bool {
	current := before.Creation
	return current.JobState == "QUEUED" && before.AvailableAtMS <= now &&
		(current.ImportState == "QUEUED" || current.ImportState == "FAILED") && before.ItemCount == 0 &&
		before.ResolvedFiles == 0
}

func decodeImportWork(before ImportWorkerSnapshot) (ImportWork, error) {
	value := before.Creation
	if !before.HasRequest || !MatchesImportDocumentDigest(value.RequestJSON, value.RequestDigest) ||
		!MatchesImportDocumentDigest(value.TargetJSON, value.TargetDigest) {
		return ImportWork{}, ErrInvalid
	}
	var request QueuedImportRequest
	var target ImportTargetSnapshot
	if err := json.Unmarshal([]byte(value.RequestJSON), &request); err != nil {
		return ImportWork{}, fmt.Errorf("%w: decode import request: %w", ErrInvalid, err)
	}
	if err := json.Unmarshal([]byte(value.TargetJSON), &target); err != nil {
		return ImportWork{}, fmt.Errorf("%w: decode import target: %w", ErrInvalid, err)
	}
	if request.SchemaVersion != 1 || target.SchemaVersion != 1 || len(target.Targets) == 0 ||
		target.PlatformInstanceID != request.Request.TargetPlatformInstanceID ||
		request.Request.UploadID != value.UploadID {
		return ImportWork{}, ErrInvalid
	}
	normalized, _, err := NormalizeImportRequest(request.Request)
	if err != nil {
		return ImportWork{}, fmt.Errorf("decode import request policy: %w", err)
	}
	execution := value.Execution
	execution.Target = target
	return ImportWork{Execution: execution, Request: normalized}, nil
}

func validateClaimUpload(ctx context.Context, facts ImportFactsReader, before ImportWorkerSnapshot) error {
	upload, found, err := facts.Upload(ctx, before.Creation.UploadID)
	if err != nil {
		return fmt.Errorf("read import claim upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.Version != before.Creation.UploadVersion ||
		upload.ManifestDigest != before.Creation.UploadDigest {
		return ErrInvalid
	}
	return nil
}

func (service *ImportExecutions) rejectQueued(
	ctx context.Context,
	scope ImportExecutionScope,
	before ImportWorkerSnapshot,
	cause error,
	now int64,
) error {
	code, retryable := ImportFailure(cause)
	change, err := importFailedTransition(before, code, retryable, now)
	if err != nil {
		return err
	}
	if err := scope.Records.Transition(ctx, change); err != nil {
		return fmt.Errorf("reject queued import: %w", err)
	}
	if !retryable {
		return scheduleImportTerminal(ctx, scope, before.Creation.Execution.ImportID, now)
	}
	return nil
}

func scheduleImportTerminal(ctx context.Context, scope ImportExecutionScope, id string, now int64) error {
	_, err := payloadrelease.NewScheduler(nil).TerminalImport(ctx, scope.Payload, id, now)
	if err != nil {
		return fmt.Errorf("schedule import execution payload: %w", err)
	}
	return nil
}

func importClaimProblem(before ImportWorkerSnapshot, now int64) error {
	if before.DeadlineAtMS != nil && *before.DeadlineAtMS <= now {
		return context.DeadlineExceeded
	}
	if before.Creation.Execution.Attempt >= before.Creation.MaxAttempts {
		return ErrInvalid
	}
	return nil
}
