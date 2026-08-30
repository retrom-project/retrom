package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/payloadrelease"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/fileset"
)

const (
	importGroupDeadline  = 6 * time.Hour
	importGroupLease     = 60 * time.Second
	importGroupHeartbeat = 15 * time.Second
)

type queuedCreationWork struct {
	importID, jobID, workerID, actorUserID string
	request                                CreateRequest
	targetSnapshot                         importGroupTargetSnapshot
	executionStartedAt, executionNo        int64
	attempt                                int
}

func (work queuedCreationWork) accepts(target creationTarget) bool {
	if target.instanceVersion != work.targetSnapshot.PlatformInstanceVersion ||
		target.platformID != work.targetSnapshot.PlatformID ||
		target.defaultCoreID != work.targetSnapshot.DefaultCoreID {
		return false
	}
	wanted := targetGuard(target)
	for _, candidate := range work.targetSnapshot.Targets {
		if candidate == wanted {
			return true
		}
	}
	return false
}

func (service *Service) scheduleImportGroup(ctx context.Context, jobID string, delay time.Duration) {
	workerContext := context.WithoutCancel(ctx)
	if delay <= 0 {
		go service.runImportGroup(workerContext, jobID)
		return
	}
	time.AfterFunc(delay, func() { service.runImportGroup(workerContext, jobID) })
}

// ResumeImportGroupJobs schedules persisted queue entries without changing
// already-running work. It is safe after manual retry and on normal startup.
func (service *Service) ResumeImportGroupJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	for _, job := range service.queuedJobRuns(ctx, "IMPORT_GROUP") {
		service.scheduleImportGroup(ctx, job.id, time.Duration(job.availableAt-now)*time.Millisecond)
	}
}

// RecoverImportGroupJobs runs once when a server process starts. Worker IDs
// are process-local, so a persisted RUNNING row cannot still have an owner.
func (service *Service) RecoverImportGroupJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	cancelledImportIDs, err := importGroupsAwaitingCancellation(ctx, transaction)
	if err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='QUEUED',last_error_code=NULL,version=version+1,updated_at_ms=?
WHERE id IN (
 SELECT scope_id FROM jobs WHERE kind='IMPORT_GROUP' AND state='RUNNING'
) AND state='RUNNING'
`, now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,version=version+1,updated_at_ms=?
WHERE kind='IMPORT_GROUP' AND state='RUNNING'
`, now, now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,'IMPORT_GROUP',scope_id,'CANCELLED',
 json_object('schemaVersion',1,'executionNo',execution_no,'attempt',attempt_count,
 'state','CANCELLED'),?
FROM jobs WHERE kind='IMPORT_GROUP' AND state='CANCEL_REQUESTED'
`, now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='CANCELLED',completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id IN (
 SELECT scope_id FROM jobs WHERE kind='IMPORT_GROUP' AND state='CANCEL_REQUESTED'
) AND state='CANCEL_REQUESTED'
`, now, now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,version=version+1,updated_at_ms=?
WHERE kind='IMPORT_GROUP' AND state='CANCEL_REQUESTED'
`, now, now); err != nil {
		return
	}
	if transaction.Commit() != nil {
		return
	}
	for _, importID := range cancelledImportIDs {
		service.scheduleTerminalImportGroupRelease(ctx, importID)
	}
	service.ResumeImportGroupJobs(ctx)
}

func importGroupsAwaitingCancellation(ctx context.Context, transaction *sql.Tx) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT scope_id FROM jobs WHERE kind='IMPORT_GROUP' AND state='CANCEL_REQUESTED' ORDER BY scope_id
`)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/group recovery: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]string, 0)
	for rows.Next() {
		var importID string
		if err := rows.Scan(&importID); err != nil {
			return nil, fmt.Errorf("libraryimport/group recovery: %w", err)
		}
		result = append(result, importID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/group recovery: %w", err)
	}
	return result, nil
}

func (service *Service) runImportGroup(ctx context.Context, jobID string) {
	select {
	case service.importGroupSlots <- struct{}{}:
		defer func() { <-service.importGroupSlots }()
	case <-ctx.Done():
		return
	}
	work, err := service.claimImportGroup(ctx, jobID)
	if err != nil {
		return
	}
	remaining := time.Duration(
		work.executionStartedAt+int64(importGroupDeadline/time.Millisecond)-service.now().UnixMilli(),
	) * time.Millisecond
	workerContext, cancel := context.WithTimeout(ctx, max(remaining, 0))
	service.registerImportGroupCancel(jobID, cancel)
	defer service.unregisterImportGroupCancel(jobID)
	heartbeatDone := make(chan struct{})
	go service.heartbeatImportGroup(workerContext, work, heartbeatDone)
	defer close(heartbeatDone)
	plan, err := service.prepareCreation(queuedPrincipalContext(workerContext, work.actorUserID), work.request)
	if err == nil {
		err = service.recordImportGroupProgress(workerContext, work, "PERSISTING", len(plan.groups))
	}
	if err == nil {
		transaction, beginErr := service.database.BeginTx(workerContext, nil)
		if beginErr != nil {
			err = fmt.Errorf("libraryimport/group worker: %w", beginErr)
		} else {
			run := newQueuedCreationRun(
				queuedPrincipalContext(workerContext, work.actorUserID), service, transaction, plan, work,
			)
			err = run.execute()
		}
	}
	if err == nil {
		return
	}
	service.finishImportGroupFailure(context.WithoutCancel(ctx), work, err)
}

func (service *Service) claimImportGroup(ctx context.Context, jobID string) (queuedCreationWork, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return queuedCreationWork{}, fmt.Errorf("libraryimport/group claim: %w", err)
	}
	defer cleanup.Rollback(transaction)
	workerUUID, _ := uuid.NewV7()
	workerID := workerUUID.String()
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
 execution_started_at_ms=COALESCE(execution_started_at_ms,?),
 execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),
 leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind='IMPORT_GROUP' AND state='QUEUED' AND available_at_ms<=?
AND attempt_count<max_attempts
`, workerID, now, now+int64(importGroupDeadline/time.Millisecond),
		now+int64(importGroupLease/time.Millisecond), now, now, jobID, now)
	if err != nil {
		return queuedCreationWork{}, fmt.Errorf("libraryimport/group claim: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return queuedCreationWork{}, ErrInvalid
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='RUNNING',last_error_code=NULL,completed_at_ms=NULL,
 version=version+1,updated_at_ms=?
WHERE id=(SELECT scope_id FROM jobs WHERE id=?) AND state IN ('QUEUED','FAILED')
`, now, jobID)
	if err != nil {
		return queuedCreationWork{}, fmt.Errorf("libraryimport/group claim: %w", err)
	}
	changed, err = result.RowsAffected()
	if err != nil || changed != 1 {
		return queuedCreationWork{}, ErrInvalid
	}
	work, err := readImportGroupWork(ctx, transaction, jobID, workerID)
	if err != nil {
		return queuedCreationWork{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'STARTED',
 json_object('schemaVersion',1,'executionNo',?,'attempt',?,'state','RUNNING',
 'phase','INSPECTING'),?)
`, jobID, work.importID, work.executionNo, work.attempt, now); err != nil {
		return queuedCreationWork{}, fmt.Errorf("libraryimport/group claim: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return queuedCreationWork{}, fmt.Errorf("libraryimport/group claim: %w", err)
	}
	return work, nil
}

func readImportGroupWork(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, workerID string,
) (queuedCreationWork, error) {
	var work queuedCreationWork
	var requestJSON, requestDigest, targetJSON, targetDigest string
	var expectedUploadVersion, currentUploadVersion int64
	var expectedManifestDigest, currentManifestDigest, uploadState string
	var actorUserID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT job.scope_id,job.execution_started_at_ms,job.execution_no,job.attempt_count,
 request.request_json,request.request_digest,
 request.actor_user_id,request.target_snapshot_json,request.target_snapshot_digest,
 request.upload_version,request.upload_manifest_digest,upload.version,upload.manifest_digest,upload.state
FROM jobs job
JOIN import_group_requests request ON request.import_job_id=job.scope_id
JOIN import_jobs import ON import.id=job.scope_id
JOIN upload_sessions upload ON upload.id=import.upload_session_id
WHERE job.id=? AND job.state='RUNNING' AND job.worker_id=?
`, jobID, workerID).Scan(
		&work.importID, &work.executionStartedAt, &work.executionNo, &work.attempt,
		&requestJSON, &requestDigest,
		&actorUserID, &targetJSON, &targetDigest, &expectedUploadVersion, &expectedManifestDigest,
		&currentUploadVersion, &currentManifestDigest, &uploadState,
	)
	if err != nil {
		return queuedCreationWork{}, fmt.Errorf("libraryimport/group input: %w", err)
	}
	if _, digest := marshaledDigest(json.RawMessage(requestJSON)); digest != requestDigest {
		return queuedCreationWork{}, ErrInvalid
	}
	if _, digest := marshaledDigest(json.RawMessage(targetJSON)); digest != targetDigest {
		return queuedCreationWork{}, ErrInvalid
	}
	if uploadState != "COMPLETE" || currentUploadVersion != expectedUploadVersion ||
		currentManifestDigest != expectedManifestDigest {
		return queuedCreationWork{}, ErrInvalid
	}
	var request queuedImportGroupRequest
	if json.Unmarshal([]byte(requestJSON), &request) != nil || request.SchemaVersion != 1 {
		return queuedCreationWork{}, ErrInvalid
	}
	if json.Unmarshal([]byte(targetJSON), &work.targetSnapshot) != nil ||
		work.targetSnapshot.SchemaVersion != 1 || len(work.targetSnapshot.Targets) == 0 {
		return queuedCreationWork{}, ErrInvalid
	}
	work.jobID, work.workerID, work.request = jobID, workerID, request.Request
	work.actorUserID = actorUserID.String
	return work, nil
}

func (service *Service) recordImportGroupProgress(
	ctx context.Context,
	work queuedCreationWork,
	phase string,
	itemCount int,
) error {
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,'IMPORT_GROUP',scope_id,'PROGRESS',
 json_object('schemaVersion',1,'executionNo',execution_no,'attempt',attempt_count,
 'phase',?,'completedUnits',?,'totalUnits',?,'unit','ITEM'),?
FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?
`, phase, itemCount, itemCount, now, work.jobID, work.workerID)
	if err != nil {
		return fmt.Errorf("libraryimport/group progress: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrInvalid
	}
	return nil
}

func (service *Service) heartbeatImportGroup(ctx context.Context, work queuedCreationWork, done <-chan struct{}) {
	ticker := time.NewTicker(importGroupHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := service.now().UnixMilli()
			_, _ = service.database.ExecContext(context.WithoutCancel(ctx), `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now+int64(importGroupLease/time.Millisecond), now, work.jobID, work.workerID)
		}
	}
}

func (service *Service) registerImportGroupCancel(jobID string, cancel context.CancelFunc) {
	service.importGroupMu.Lock()
	defer service.importGroupMu.Unlock()
	service.importGroupCancels[jobID] = cancel
}

func (service *Service) unregisterImportGroupCancel(jobID string) {
	service.importGroupMu.Lock()
	defer service.importGroupMu.Unlock()
	if cancel, exists := service.importGroupCancels[jobID]; exists {
		cancel()
		delete(service.importGroupCancels, jobID)
	}
}

func (service *Service) CancelImportGroupJob(jobID string) {
	service.importGroupMu.Lock()
	defer service.importGroupMu.Unlock()
	if cancel, exists := service.importGroupCancels[jobID]; exists {
		cancel()
	}
}

// SyncImportGroupCancellation mirrors a generic queued-job cancellation into
// its aggregate. Running jobs are completed by their registered worker.
func (service *Service) SyncImportGroupCancellation(ctx context.Context, jobID string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	var importID, jobState, importState string
	if err := transaction.QueryRowContext(ctx, `
SELECT job.scope_id,job.state,import.state
FROM jobs job JOIN import_jobs import ON import.id=job.scope_id
WHERE job.id=? AND job.kind='IMPORT_GROUP'
`, jobID).Scan(&importID, &jobState, &importState); err != nil || jobState != "CANCELLED" {
		return
	}
	if importState == "CANCELLED" {
		return
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='CANCELLED',cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),
 cancel_reason=COALESCE(cancel_reason,'任务已取消'),completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('QUEUED','FAILED')
`, now, now, now, importID); err != nil {
		return
	}
	if _, err := payloadrelease.ScheduleTerminalImportJob(ctx, transaction, importID, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) finishImportGroupFailure(ctx context.Context, work queuedCreationWork, cause error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	var state string
	var attempt, maxAttempts int
	if err := transaction.QueryRowContext(ctx, `
SELECT state,attempt_count,max_attempts FROM jobs WHERE id=? AND worker_id=?
`, work.jobID, work.workerID).Scan(&state, &attempt, &maxAttempts); err != nil {
		return
	}
	if state == "CANCEL_REQUESTED" || errors.Is(cause, context.Canceled) {
		service.finishImportGroupCancellation(ctx, transaction, work)
		return
	}
	code, retryable := importGroupFailure(cause)
	now := service.now().UnixMilli()
	if retryable && attempt < maxAttempts {
		service.rescheduleImportGroupFailure(ctx, transaction, work, code, attempt, now)
		return
	}
	if !persistTerminalImportGroupFailure(
		ctx, transaction, work, code, retryable, attempt, now,
	) {
		return
	}
	if !retryable {
		service.scheduleTerminalImportGroupRelease(ctx, work.importID)
	}
}

func (service *Service) rescheduleImportGroupFailure(
	ctx context.Context,
	transaction *sql.Tx,
	work queuedCreationWork,
	code string,
	attempt int,
	now int64,
) {
	delay := importGroupRetryDelay(attempt)
	availableAt := now + int64(delay/time.Millisecond)
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,error_code=?,error_retryable=1,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, availableAt, code, now, work.jobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='QUEUED',last_error_code=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, code, now, work.importID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'RETRY_SCHEDULED',
 json_object('schemaVersion',1,'executionNo',?,'attempt',?,'errorCode',?,
 'errorRetryable',1,'availableAtMs',?),?)
`, work.jobID, work.importID, work.executionNo, attempt, code, availableAt, now); err != nil {
		return
	}
	if transaction.Commit() == nil {
		service.scheduleImportGroup(ctx, work.jobID, delay)
	}
}

func persistTerminalImportGroupFailure(
	ctx context.Context,
	transaction *sql.Tx,
	work queuedCreationWork,
	code string,
	retryable bool,
	attempt int,
	now int64,
) bool {
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,error_code=?,error_retryable=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
	`, now, code, retryable, now, work.jobID); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='FAILED',last_error_code=?,completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
	`, code, now, now, work.importID); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'FAILED',json_object('schemaVersion',1,'executionNo',?,
 'attempt',?,'errorCode',?,'errorRetryable',?),?)
`, work.jobID, work.importID, work.executionNo, attempt, code, retryable, now); err != nil {
		return false
	}
	if transaction.Commit() != nil {
		return false
	}
	return true
}

func (service *Service) finishImportGroupCancellation(
	ctx context.Context,
	transaction *sql.Tx,
	work queuedCreationWork,
) {
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('RUNNING','CANCEL_REQUESTED')
`, now, now, work.jobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET state='CANCELLED',completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('RUNNING','CANCEL_REQUESTED')
`, now, now, work.importID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'CANCELLED',json_object('schemaVersion',1,'executionNo',?,
 'attempt',?,'state','CANCELLED'),?)
`, work.jobID, work.importID, work.executionNo, work.attempt, now); err != nil {
		return
	}
	if transaction.Commit() == nil {
		service.scheduleTerminalImportGroupRelease(ctx, work.importID)
	}
}

func importGroupRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Second
	case 2:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

func (service *Service) scheduleTerminalImportGroupRelease(ctx context.Context, importID string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := payloadrelease.ScheduleTerminalImportJob(
		ctx, transaction, importID, service.now().UnixMilli(),
	); err != nil {
		return
	}
	_ = transaction.Commit()
}

func importGroupFailure(cause error) (string, bool) {
	if errors.Is(cause, context.DeadlineExceeded) {
		return "IMPORT_GROUP_EXECUTION_TIMEOUT", true
	}
	var detectionError *detector.Error
	if errors.As(cause, &detectionError) {
		return string(detectionError.Code), false
	}
	var projectError *fileset.ProjectError
	if errors.As(cause, &projectError) {
		return string(projectError.Code), false
	}
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{importing.ErrArchiveLimitExceeded, "ARCHIVE_LIMIT_EXCEEDED"},
		{importing.ErrArchiveEncrypted, "ARCHIVE_ENCRYPTED_UNSUPPORTED"},
		{importing.ErrArchiveVolumeUnsupported, "ARCHIVE_VOLUME_UNSUPPORTED"},
		{importing.ErrArchiveCasefoldCollision, "RPG_PATH_COLLISION"},
		{ErrMultiDiscPlaylistMissing, "MULTI_DISC_PLAYLIST_MISSING"},
		{ErrMultiDiscModeUnavailable, "MULTI_DISC_MODE_UNAVAILABLE"},
		{ErrInvalid, "IMPORT_INPUT_INVALID"},
	} {
		if errors.Is(cause, candidate.err) {
			return candidate.code, false
		}
	}
	return "IMPORT_GROUP_FAILED", true
}
