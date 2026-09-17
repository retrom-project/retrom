package uploads

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	service "retrom/internal/model/uploads"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) CommitCreateSession(ctx context.Context, reg service.Registration) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	if err := scope.Sessions.Create(ctx, reg); err != nil {
		return fmt.Errorf("create upload session: %w", err)
	}
	return commitScope(tx)
}

func (repository *Repository) CommitRecordPart(ctx context.Context, cmd service.RecordPartCommand) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	target, err := scope.Files.Target(ctx, cmd.Key)
	if err != nil {
		return fmt.Errorf("recheck upload part target: %w", err)
	}
	if err := validatePartTarget(target, cmd.TotalBytes, cmd.NowMS); err != nil {
		return err
	}
	if target.SessionState == "FAILED" {
		allowed, err := scope.Finalize.Repair(ctx, cmd.Key, cmd.Part.Part.Number)
		if err != nil {
			return fmt.Errorf("read upload repair authorization: %w", err)
		}
		if !allowed {
			return service.ErrInvalid
		}
	}
	inserted, err := scope.Parts.Put(ctx, cmd.Part)
	if err != nil {
		return fmt.Errorf("write upload part: %w", err)
	}
	if !inserted {
		return checkPartReplay(ctx, scope, cmd.Part, tx)
	}
	if err := scope.Files.AddReceived(ctx, service.FileProgress{
		FileID: cmd.Part.FileID, Bytes: cmd.Part.Part.Size, AtMS: cmd.NowMS,
	}); err != nil {
		return fmt.Errorf("advance upload file: %w", err)
	}
	if err := scope.Sessions.Advance(ctx, service.SessionProgress{
		ID: target.UploadID, State: "UPLOADING", ExpectedVersion: target.SessionVersion, AtMS: cmd.NowMS,
	}); err != nil {
		return fmt.Errorf("advance upload session: %w", err)
	}
	return commitScope(tx)
}

func checkPartReplay(ctx context.Context, scope service.WriteScope, part service.PartRecord, tx *sql.Tx) error {
	existing, found, err := scope.Parts.Get(ctx, part.FileID, part.Part.Number)
	if err != nil {
		return fmt.Errorf("read upload part replay: %w", err)
	}
	if !found || existing.Part.SHA256 != part.Part.SHA256 ||
		existing.Part.Offset != part.Part.Offset || existing.Part.Size != part.Part.Size {
		return service.ErrInvalid
	}
	return commitScope(tx)
}

func validatePartTarget(target service.PartTarget, total, now int64) error {
	if target.DeclaredSize != total || target.FileState == "COMPLETE" || target.FileState == "FINALIZING" ||
		now >= target.ExpiresAtMS {
		return service.ErrInvalid
	}
	if target.SessionState == "CREATED" || target.SessionState == "UPLOADING" {
		return nil
	}
	if target.SessionState == "FAILED" && target.LastErrorCode != nil &&
		(*target.LastErrorCode == "UPLOAD_PART_MISSING" || *target.LastErrorCode == "UPLOAD_PART_CORRUPT") {
		return nil
	}
	return service.ErrInvalid
}

func (repository *Repository) CommitRepairPart(ctx context.Context, key service.FileKey, number int) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	target, err := scope.Files.Target(ctx, key)
	if err != nil {
		return fmt.Errorf("check upload repair: %w", err)
	}
	if target.SessionState != "FAILED" {
		return commitScope(tx)
	}
	if target.LastErrorCode == nil || *target.LastErrorCode != "UPLOAD_PART_CORRUPT" {
		return service.ErrInvalid
	}
	repaired, err := scope.Finalize.Repair(ctx, key, number)
	if err != nil {
		return fmt.Errorf("repair upload part: %w", err)
	}
	if !repaired {
		return service.ErrInvalid
	}
	return commitScope(tx)
}

func (repository *Repository) CommitComplete(
	ctx context.Context, cmd service.CompleteCommand,
) (service.Run, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return service.Run{}, err
	}
	defer dbexec.Rollback(tx)
	current, err := scope.Sessions.Current(ctx, cmd.UploadID)
	if err != nil {
		return service.Run{}, fmt.Errorf("read upload completion state: %w", err)
	}
	if current.Version != cmd.Version || current.Consumed ||
		current.State != "UPLOADING" && current.State != "CREATED" && current.State != "FAILED" {
		return service.Run{}, service.ErrInvalid
	}
	files, err := scope.Finalize.Manifest(ctx, cmd.UploadID)
	if err != nil {
		return service.Run{}, fmt.Errorf("freeze upload parts: %w", err)
	}
	job, err := buildFinalizationJob(
		cmd.UploadID, current.FinalizationNo+1, cmd.NowMS, files, cmd.JobID, cmd.ExecutionID,
	)
	if err != nil {
		return service.Run{}, err
	}
	if err := scope.Jobs.Create(ctx, job); err != nil {
		return service.Run{}, fmt.Errorf("create finalize job: %w", err)
	}
	if err := scope.Sessions.BeginFinalization(ctx, service.Finalization{
		Run: job.Run, ExpectedVersion: cmd.Version, AtMS: job.AtMS,
	}); err != nil {
		return service.Run{}, fmt.Errorf("begin upload finalization: %w", err)
	}
	if err := scope.Files.MarkFinalizing(ctx, cmd.UploadID, job.AtMS); err != nil {
		return service.Run{}, fmt.Errorf("mark upload files finalizing: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return service.Run{}, err
	}
	return job.Run, nil
}

func buildFinalizationJob(
	uploadID string, number, now int64, files []service.FrozenFile, jobID, executionID string,
) (service.JobCreation, error) {
	run := service.Run{UploadID: uploadID, JobID: jobID, FinalizationNo: number, ExecutionNo: 1}
	envelope := service.FinalizationInput{SchemaVersion: 1, Kind: "UPLOAD_FINALIZE", ExecutionID: executionID}
	envelope.Scope.Type = "UPLOAD_SESSION"
	envelope.Scope.ID = uploadID
	envelope.Inputs.FinalizationNo = number
	envelope.Inputs.Files = files
	input, err := json.Marshal(envelope)
	if err != nil {
		return service.JobCreation{}, fmt.Errorf("encode finalization input: %w", err)
	}
	inputDigest := sha256.Sum256(input)
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", uploadID, number)))
	return service.JobCreation{
		Run: run, DedupeKey: hex.EncodeToString(dedupe[:]),
		InputDigest: hex.EncodeToString(inputDigest[:]),
		InputJSON:   input,
		PayloadJSON: []byte(fmt.Sprintf(`{"uploadId":%q,"finalizationNo":%d}`, uploadID, number)),
		EventJSON:   buildJobEvent(run), AtMS: now,
	}, nil
}

func buildJobEvent(run service.Run) []byte {
	data, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "executionNo": run.ExecutionNo, "attempt": 0, "errorCode": "",
	})
	return data
}

func (repository *Repository) CommitCancel(
	ctx context.Context, cmd service.CancelCommand,
) (service.CancelResult, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return service.CancelResult{}, err
	}
	defer dbexec.Rollback(tx)
	current, err := scope.Sessions.Current(ctx, cmd.UploadID)
	if err != nil {
		return service.CancelResult{}, fmt.Errorf("read upload cancellation state: %w", err)
	}
	if current.Version != cmd.Version || current.Consumed || current.State == "COMPLETE" ||
		current.State == "CANCELLED" || current.State == "EXPIRED" {
		return service.CancelResult{}, service.ErrInvalid
	}
	pending := false
	if current.State == "FINALIZING" {
		var reqErr error
		pending, reqErr = requestFinalizeCancellation(ctx, scope.Jobs, current, cmd.NowMS)
		if reqErr != nil {
			return service.CancelResult{}, reqErr
		}
	}
	if pending {
		if err := scope.Sessions.Advance(ctx, service.SessionProgress{
			ID: cmd.UploadID, State: current.State,
			ExpectedVersion: cmd.Version, AtMS: cmd.NowMS,
		}); err != nil {
			return service.CancelResult{}, fmt.Errorf("persist upload transition: %w", err)
		}
	} else {
		run := service.Run{UploadID: cmd.UploadID}
		if err := finishUploadCancellation(ctx, scope, run, cmd.Version, cmd.NowMS); err != nil {
			return service.CancelResult{}, err
		}
	}
	result := service.CancelResult{
		Result:  service.Canceled{UploadID: cmd.UploadID, State: "CANCELLED", Version: cmd.Version + 1},
		Pending: pending,
	}
	if pending {
		result.Result.State = "CANCEL_REQUESTED"
	}
	if err := commitScope(tx); err != nil {
		return service.CancelResult{}, err
	}
	return result, nil
}

func requestFinalizeCancellation(
	ctx context.Context, records service.JobRecords, current service.SessionState, now int64,
) (bool, error) {
	if current.FinalizeJobID == nil {
		return false, service.ErrInvalid
	}
	job, err := records.Get(ctx, *current.FinalizeJobID)
	if err != nil {
		return false, fmt.Errorf("read finalize cancellation target: %w", err)
	}
	if job.State == "CANCEL_REQUESTED" {
		return true, nil
	}
	if job.State == "CANCELLED" {
		return false, nil
	}
	if job.State != "QUEUED" && job.State != "RUNNING" {
		return false, service.ErrInvalid
	}
	input := service.JobCancellation{
		ID: job.ID, UploadID: current.ID, ExpectedState: job.State,
		State: "CANCEL_REQUESTED", AtMS: now, ExecutionNo: job.ExecutionNo,
	}
	if job.State == "QUEUED" {
		input.State = "CANCELLED"
		input.FinishedAtMS = &now
	}
	input.EventJSON = buildJobEvent(service.Run{ExecutionNo: job.ExecutionNo})
	if err := records.RequestCancel(ctx, input); err != nil {
		return false, fmt.Errorf("request upload cancellation: %w", err)
	}
	return job.State == "RUNNING", nil
}

func finishUploadCancellation(
	ctx context.Context, scope service.WriteScope, run service.Run, version, now int64,
) error {
	code := "UPLOAD_CANCELLED"
	if err := scope.Sessions.Finish(ctx, service.SessionFinish{
		Run: run, State: "CANCELLED", ExpectedVersion: version, AtMS: now, ErrorCode: &code,
	}); err != nil {
		return fmt.Errorf("persist upload transition: %w", err)
	}
	if err := scope.Files.FailPending(ctx, service.PendingFailure{
		UploadID: run.UploadID, Code: code, AtMS: now,
	}); err != nil {
		return fmt.Errorf("fail pending upload files: %w", err)
	}
	return nil
}

func (repository *Repository) CommitClaimFinalization(
	ctx context.Context, cmd service.ClaimFinalizationCommand,
) (service.ClaimResult, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return service.ClaimResult{}, err
	}
	defer dbexec.Rollback(tx)
	result, err := claimFinalization(ctx, scope, cmd)
	if err != nil {
		return service.ClaimResult{}, err
	}
	if err := commitScope(tx); err != nil {
		return service.ClaimResult{}, err
	}
	return result, nil
}

func claimFinalization(
	ctx context.Context, scope service.WriteScope, cmd service.ClaimFinalizationCommand,
) (service.ClaimResult, error) {
	job, err := scope.Jobs.Get(ctx, cmd.JobID)
	if err != nil {
		return service.ClaimResult{}, fmt.Errorf("read upload authority: %w", err)
	}
	if job.Kind != "UPLOAD_FINALIZE" || job.Scope != "UPLOAD_SESSION" {
		return service.ClaimResult{}, service.ErrExecutionLost
	}
	current, err := scope.Sessions.Current(ctx, job.ScopeID)
	if err != nil {
		return service.ClaimResult{}, fmt.Errorf("read upload authority: %w", err)
	}
	if current.FinalizeJobID == nil || *current.FinalizeJobID != cmd.JobID || current.Consumed {
		return service.ClaimResult{}, nil
	}
	run := service.Run{
		UploadID: current.ID, JobID: cmd.JobID, FinalizationNo: current.FinalizationNo,
		ExecutionNo: job.ExecutionNo, WorkerID: cmd.WorkerID,
		Attempt: job.Attempt + 1, Deadline: job.Deadline,
	}
	result := service.ClaimResult{Run: run}
	if job.State == "CANCELLED" && matchesRun(current, run) {
		result.Cancelled = true
		if err := finishUploadCancellation(ctx, scope, run, current.Version, cmd.NowMS); err != nil {
			return service.ClaimResult{}, err
		}
		return result, nil
	}
	if err := claimCurrent(ctx, scope, current, job, &result, cmd.NowMS); err != nil {
		return service.ClaimResult{}, err
	}
	return result, nil
}

func matchesRun(current service.SessionState, run service.Run) bool {
	return current.ID == run.UploadID && current.State == "FINALIZING" && current.FinalizeJobID != nil &&
		*current.FinalizeJobID == run.JobID && current.FinalizationNo == run.FinalizationNo
}

func claimCurrent(
	ctx context.Context, scope service.WriteScope,
	current service.SessionState, job service.Job, result *service.ClaimResult, now int64,
) error {
	run := &result.Run
	skip, err := tryShortCircuitActiveJob(ctx, scope, job, run, now)
	if err != nil || skip {
		return err
	}
	switch current.State {
	case "FINALIZING", "FAILED":
	default:
		return nil
	}
	result.Input, result.Cause = service.DecodeFinalization(job)
	if result.Cause == nil && result.Input.Inputs.FinalizationNo != current.FinalizationNo {
		result.Cause = service.ErrExecutionLost
	}
	if result.Cause == nil {
		files, err := scope.Finalize.Manifest(ctx, current.ID)
		if err != nil {
			return fmt.Errorf("read frozen upload files: %w", err)
		}
		result.Cause = service.ValidateFinalizationFiles(result.Input, files)
	}
	claimOverrideCause(job, result, run, now)
	if result.Cause == nil && job.Available > now {
		return nil
	}
	return issueClaim(ctx, scope, current, job, result, run, now)
}

func tryShortCircuitActiveJob(
	ctx context.Context, scope service.WriteScope, job service.Job, run *service.Run, now int64,
) (bool, error) {
	if job.State != "RUNNING" && job.State != "CANCEL_REQUESTED" {
		return job.State != "QUEUED", nil
	}
	if job.Lease > now && job.Deadline > now {
		return true, nil
	}
	run.Attempt = job.Attempt
	if job.State == "RUNNING" && job.Deadline > now && job.Attempt < job.MaxAttempts {
		if err := scope.Leases.Requeue(ctx, job, now, min(now+1000, job.Deadline)); err != nil {
			return false, fmt.Errorf("requeue finalization: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func claimOverrideCause(job service.Job, result *service.ClaimResult, run *service.Run, now int64) {
	switch {
	case job.State == "CANCEL_REQUESTED":
		result.Cause = context.Canceled
	case job.Deadline > 0 && job.Deadline <= now:
		result.Cause = context.DeadlineExceeded
	case job.Attempt >= job.MaxAttempts:
		result.Cause = service.ErrAttemptsExhausted
	}
	if result.Cause != nil {
		run.Attempt = job.Attempt
	}
	if run.Deadline == 0 {
		run.Deadline = now + (10 * time.Minute).Milliseconds()
	}
}

func issueClaim(
	ctx context.Context, scope service.WriteScope,
	current service.SessionState, job service.Job, result *service.ClaimResult, run *service.Run, now int64,
) error {
	if current.State == "FAILED" {
		if err := scope.Sessions.Advance(ctx, service.SessionProgress{
			ID: current.ID, State: "FINALIZING", ExpectedVersion: current.Version, AtMS: now,
		}); err != nil {
			return fmt.Errorf("resume upload session: %w", err)
		}
		if err := scope.Files.MarkFinalizing(ctx, current.ID, now); err != nil {
			return fmt.Errorf("resume upload files: %w", err)
		}
	}
	claimed, err := scope.Jobs.Claim(ctx, service.JobClaim{
		Run: *run, Version: job.Version, AtMS: now,
		EventJSON: buildJobEvent(*run),
	})
	if err != nil {
		return fmt.Errorf("claim upload finalization job: %w", err)
	}
	result.Acquired = claimed
	return nil
}

func (repository *Repository) CommitObserveFinalization(
	ctx context.Context, cmd service.ObserveCommand,
) error {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	job, err := scope.Jobs.Get(ctx, cmd.Run.JobID)
	if err != nil {
		return fmt.Errorf("observe upload job: %w", err)
	}
	if job.State == "CANCEL_REQUESTED" && executionOwned(job, cmd.Run) {
		return context.Canceled
	}
	if err := executionActive(job, cmd.Run, cmd.NowMS); err != nil {
		return fmt.Errorf("observe upload job: %w", err)
	}
	if cmd.NowMS-cmd.LastMS >= 15000 {
		if err := scope.Leases.Refresh(ctx, cmd.Run, cmd.NowMS); err != nil {
			return fmt.Errorf("refresh upload lease: %w", err)
		}
	}
	return commitScope(tx)
}

func executionOwned(job service.Job, run service.Run) bool {
	return job.ExecutionNo == run.ExecutionNo && job.WorkerID == run.WorkerID
}

func executionActive(job service.Job, run service.Run, now int64) error {
	if !executionOwned(job, run) {
		return service.ErrExecutionLost
	}
	if job.State != "RUNNING" && job.State != "CANCEL_REQUESTED" {
		return service.ErrExecutionLost
	}
	if job.Lease <= now || job.Deadline <= now {
		return service.ErrExecutionLost
	}
	return nil
}

func (repository *Repository) CommitReadCandidates(
	ctx context.Context, cmd service.FinalizationOwnershipCommand,
) ([]service.Candidate, bool, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return nil, false, err
	}
	defer dbexec.Rollback(tx)
	stopped, err := checkFinalizationOwnership(ctx, scope, cmd)
	if err != nil || stopped {
		return nil, stopped, err
	}
	files, err := scope.Finalize.Candidates(ctx, cmd.Run.UploadID)
	if err != nil {
		return nil, false, fmt.Errorf("read unfinished upload files: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return nil, false, err
	}
	return files, false, nil
}

func checkFinalizationOwnership(
	ctx context.Context, scope service.WriteScope, cmd service.FinalizationOwnershipCommand,
) (bool, error) {
	current, err := scope.Sessions.Current(ctx, cmd.Run.UploadID)
	if err != nil {
		return false, fmt.Errorf("read finalize owner: %w", err)
	}
	if !matchesRun(current, cmd.Run) {
		return true, nil
	}
	job, err := scope.Jobs.Get(ctx, cmd.Run.JobID)
	if err != nil {
		return false, fmt.Errorf("read finalize execution: %w", err)
	}
	if !executionOwned(job, cmd.Run) {
		return true, nil
	}
	if err := executionActive(job, cmd.Run, cmd.NowMS); err != nil {
		return false, err
	}
	return false, nil
}

func (repository *Repository) CommitPublishFile(
	ctx context.Context, cmd service.PublishFileCommand,
) (bool, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return false, err
	}
	defer dbexec.Rollback(tx)
	stopped, err := checkFinalizationOwnership(ctx, scope, service.FinalizationOwnershipCommand{
		Run: cmd.Run, NowMS: cmd.NowMS,
	})
	if err != nil || stopped {
		return stopped, err
	}
	blobID, err := scope.Blobs.Ensure(ctx, cmd.Metadata, cmd.NowMS)
	if err != nil {
		return false, fmt.Errorf("register finalized upload: %w", err)
	}
	if err := scope.Files.Publish(ctx, service.FilePublication{
		Run: cmd.Run, FileID: cmd.FileID, BlobID: blobID, AtMS: cmd.NowMS,
	}); err != nil {
		return false, fmt.Errorf("publish upload file: %w", err)
	}
	if err := scope.Parts.DeleteForFile(ctx, cmd.FileID); err != nil {
		return false, fmt.Errorf("delete upload parts: %w", err)
	}
	return false, commitScope(tx)
}

func (repository *Repository) CommitFinishFinalization(
	ctx context.Context, cmd service.FinishFinalizationCommand,
) (bool, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return false, err
	}
	defer dbexec.Rollback(tx)
	current, err := scope.Sessions.Current(ctx, cmd.Run.UploadID)
	if err != nil {
		return false, fmt.Errorf("read finalize owner: %w", err)
	}
	if !matchesRun(current, cmd.Run) {
		return true, nil
	}
	job, err := scope.Jobs.Get(ctx, cmd.Run.JobID)
	if err != nil {
		return false, fmt.Errorf("read finalize execution: %w", err)
	}
	if !executionOwned(job, cmd.Run) {
		return true, nil
	}
	if err := executionActive(job, cmd.Run, cmd.NowMS); err != nil {
		return false, err
	}
	count, err := scope.Finalize.Count(ctx, cmd.Run.UploadID)
	if err != nil {
		return false, fmt.Errorf("count unfinished upload files: %w", err)
	}
	if count != 0 {
		return true, nil
	}
	expires := cmd.NowMS + (7 * 24 * time.Hour).Milliseconds()
	if err := scope.Sessions.Finish(ctx, service.SessionFinish{
		Run: cmd.Run, State: "COMPLETE", ExpectedVersion: current.Version, AtMS: cmd.NowMS, ExpiresAtMS: &expires,
	}); err != nil {
		return false, fmt.Errorf("complete upload session: %w", err)
	}
	if err := scope.Jobs.Finish(ctx, service.JobFinish{
		Run: cmd.Run, ExpectedState: "RUNNING", State: "SUCCEEDED", AtMS: cmd.NowMS,
		EventJSON: buildJobEvent(cmd.Run),
	}); err != nil {
		return false, fmt.Errorf("complete finalize job: %w", err)
	}
	return false, commitScope(tx)
}

func (repository *Repository) CommitFinalizationFailure(
	ctx context.Context, cmd service.FinalizationFailureCommand,
) (bool, error) {
	tx, scope, err := repository.beginScope(ctx)
	if err != nil {
		return false, err
	}
	defer dbexec.Rollback(tx)
	current, err := scope.Sessions.Current(ctx, cmd.Run.UploadID)
	if err != nil {
		return false, fmt.Errorf("read finalize owner: %w", err)
	}
	if !matchesRun(current, cmd.Run) {
		return false, nil
	}
	job, err := scope.Jobs.Get(ctx, cmd.Run.JobID)
	if err != nil {
		return false, fmt.Errorf("read finalize execution: %w", err)
	}
	if !executionOwned(job, cmd.Run) || job.State != "RUNNING" && job.State != "CANCEL_REQUESTED" ||
		job.Lease <= cmd.NowMS && job.Deadline > cmd.NowMS {
		return false, nil
	}
	resolution := resolveFailure(job, cmd)
	if err := persistFailure(ctx, scope, current, job, cmd, resolution); err != nil {
		return false, err
	}
	if err := commitScope(tx); err != nil {
		return false, err
	}
	return resolution.cancelled, nil
}

type failureResolution struct {
	state     string
	code      string
	retryable bool
	cancelled bool
}

func resolveFailure(job service.Job, cmd service.FinalizationFailureCommand) failureResolution {
	cancelled := job.State == "CANCEL_REQUESTED"
	code, retryable := finalizationFailureCode(cmd.Cause, cmd.Run.Deadline, cmd.NowMS)
	state := "FAILED"
	if cancelled {
		state = "CANCELLED"
		code = "UPLOAD_CANCELLED"
		retryable = false
	}
	return failureResolution{state: state, code: code, retryable: retryable, cancelled: cancelled}
}

func persistFailure(
	ctx context.Context, scope service.WriteScope,
	current service.SessionState, job service.Job,
	cmd service.FinalizationFailureCommand, res failureResolution,
) error {
	var broken *service.BrokenPart
	if res.state == "FAILED" && errors.As(cmd.Cause, &broken) {
		if err := scope.Finalize.Invalidate(ctx, *broken, cmd.NowMS); err != nil {
			return fmt.Errorf("invalidate failed part: %w", err)
		}
	}
	if err := scope.Sessions.Finish(ctx, service.SessionFinish{
		Run: cmd.Run, State: res.state, ExpectedVersion: current.Version, AtMS: cmd.NowMS, ErrorCode: &res.code,
	}); err != nil {
		return fmt.Errorf("fail upload session: %w", err)
	}
	if err := scope.Files.FailPending(ctx, service.PendingFailure{
		UploadID: cmd.Run.UploadID, Code: res.code, AtMS: cmd.NowMS,
	}); err != nil {
		return fmt.Errorf("fail pending upload files: %w", err)
	}
	if err := scope.Jobs.Finish(ctx, service.JobFinish{
		Run: cmd.Run, ExpectedState: job.State, State: res.state, ErrorCode: &res.code, Retryable: res.retryable,
		AtMS: cmd.NowMS, EventJSON: finalizationFailureEvent(cmd.Run, res.code, cmd.Cause),
	}); err != nil {
		return fmt.Errorf("fail finalize job: %w", err)
	}
	return nil
}

func finalizationFailureCode(cause error, deadline, now int64) (string, bool) {
	if errors.Is(cause, context.Canceled) {
		return "UPLOAD_CANCELLED", false
	}
	if errors.Is(cause, context.DeadlineExceeded) || (deadline > 0 && now >= deadline) {
		return "UPLOAD_FINALIZE_TIMEOUT", true
	}
	var broken *service.BrokenPart
	if errors.As(cause, &broken) {
		if broken.Missing {
			return "UPLOAD_PART_MISSING", false
		}
		return "UPLOAD_PART_CORRUPT", false
	}
	if errors.Is(cause, service.ErrInputInvalid) {
		return "UPLOAD_INPUT_INVALID", false
	}
	if errors.Is(cause, service.ErrAttemptsExhausted) {
		return "UPLOAD_ATTEMPTS_EXHAUSTED", true
	}
	return "UPLOAD_FINALIZE_IO", true
}

func finalizationFailureEvent(run service.Run, code string, cause error) []byte {
	payload := map[string]any{
		"schemaVersion": 1, "executionNo": run.ExecutionNo,
		"attempt": run.Attempt, "errorCode": code,
	}
	var part *service.BrokenPart
	if errors.As(cause, &part) {
		payload["failedPart"] = part
	}
	data, _ := json.Marshal(payload)
	return data
}
