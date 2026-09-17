package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/service/payloadrelease"
)

type ImportExecutions struct {
	repository model.ImportExecutionRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewImportExecutions(repository model.ImportExecutionRepository, now func() time.Time) *ImportExecutions {
	if now == nil {
		now = time.Now
	}
	return &ImportExecutions{repository: repository, now: now, newID: newImportAdmissionID}
}

func (service *ImportExecutions) Claim(ctx context.Context, id string) (model.ImportWork, bool, error) {
	workerID, err := service.newID()
	if err != nil {
		return model.ImportWork{}, false, fmt.Errorf("allocate import worker: %w", err)
	}
	var result model.ImportWork
	var found bool
	var problem error
	err = service.repository.WithExecution(
		ctx,
		func(scope model.ImportExecutionScope) error {
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
				if !errors.Is(err, model.ErrInvalid) {
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
				return model.ErrVersionConflict
			}
			result, found = work, true
			return nil
		},
	)
	if err != nil {
		return model.ImportWork{}, false, fmt.Errorf("claim import worker: %w", err)
	}
	if problem != nil {
		return model.ImportWork{}, false, fmt.Errorf("reject import work: %w", problem)
	}
	return result, found, nil
}

func importClaimAvailable(before model.ImportWorkerSnapshot, now int64) bool {
	current := before.Creation
	return current.JobState == "QUEUED" && before.AvailableAtMS <= now &&
		(current.ImportState == "QUEUED" || current.ImportState == "FAILED") && before.ItemCount == 0 &&
		before.ResolvedFiles == 0
}

func decodeImportWork(before model.ImportWorkerSnapshot) (model.ImportWork, error) {
	value := before.Creation
	if !before.HasRequest || !MatchesImportDocumentDigest(value.RequestJSON, value.RequestDigest) ||
		!MatchesImportDocumentDigest(value.TargetJSON, value.TargetDigest) {
		return model.ImportWork{}, model.ErrInvalid
	}
	var request model.QueuedImportRequest
	var target model.ImportTargetSnapshot
	if err := json.Unmarshal([]byte(value.RequestJSON), &request); err != nil {
		return model.ImportWork{}, fmt.Errorf("%w: decode import request: %w", model.ErrInvalid, err)
	}
	if err := json.Unmarshal([]byte(value.TargetJSON), &target); err != nil {
		return model.ImportWork{}, fmt.Errorf("%w: decode import target: %w", model.ErrInvalid, err)
	}
	if request.SchemaVersion != 1 || target.SchemaVersion != 1 || len(target.Targets) == 0 ||
		target.PlatformInstanceID != request.Request.TargetPlatformInstanceID ||
		request.Request.UploadID != value.UploadID {
		return model.ImportWork{}, model.ErrInvalid
	}
	normalized, _, err := NormalizeImportRequest(request.Request)
	if err != nil {
		return model.ImportWork{}, fmt.Errorf("decode import request policy: %w", err)
	}
	execution := value.Execution
	execution.Target = target
	return model.ImportWork{Execution: execution, Request: normalized}, nil
}

func validateClaimUpload(ctx context.Context, facts model.ImportFactsReader, before model.ImportWorkerSnapshot) error {
	upload, found, err := facts.Upload(ctx, before.Creation.UploadID)
	if err != nil {
		return fmt.Errorf("read import claim upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.Version != before.Creation.UploadVersion ||
		upload.ManifestDigest != before.Creation.UploadDigest {
		return model.ErrInvalid
	}
	return nil
}

func (service *ImportExecutions) rejectQueued(
	ctx context.Context,
	scope model.ImportExecutionScope,
	before model.ImportWorkerSnapshot,
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

func scheduleImportTerminal(ctx context.Context, scope model.ImportExecutionScope, id string, now int64) error {
	_, err := payloadrelease.NewScheduler(nil).TerminalImport(ctx, scope.Payload, id, now)
	if err != nil {
		return fmt.Errorf("schedule import execution payload: %w", err)
	}
	return nil
}

func importClaimProblem(before model.ImportWorkerSnapshot, now int64) error {
	if before.DeadlineAtMS != nil && *before.DeadlineAtMS <= now {
		return context.DeadlineExceeded
	}
	if before.Creation.Execution.Attempt >= before.Creation.MaxAttempts {
		return model.ErrInvalid
	}
	return nil
}
