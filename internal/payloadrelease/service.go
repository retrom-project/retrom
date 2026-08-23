package payloadrelease

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
)

const executionTimeout = 30 * time.Minute

var errGCRetentionInvalid = errors.New("GC_RETENTION_INVALID")

type Service struct {
	database  *sql.DB
	blobs     *blobstore.Store
	now       func() time.Time
	waitFor   func(context.Context, time.Duration) error
	retention time.Duration
	stop      chan struct{}
	wake      chan struct{}
	wait      sync.WaitGroup
	closeOnce sync.Once
}

type claimedJob struct {
	ID, ScopeID string
	ScopeType   ScopeType
	Attempt     int64
	Input       scheduleInput
}

func New(database *sql.DB, blobs *blobstore.Store, now func() time.Time, retention time.Duration) (*Service, error) {
	if retention < 24*time.Hour || retention > 30*24*time.Hour {
		return nil, errGCRetentionInvalid
	}
	if err := ValidateOwnershipRegistry(); err != nil {
		return nil, err
	}
	if err := validateLifecycleState(context.Background(), database); err != nil {
		return nil, err
	}
	return &Service{
		database: database, blobs: blobs, now: now, waitFor: waitForContext, retention: retention,
		stop: make(chan struct{}), wake: make(chan struct{}, 1),
	}, nil
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("payloadrelease/wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (service *Service) Start() {
	_ = service.recoverInterruptedJobs(context.Background())
	_ = service.releaseExpiredProviderPayloads(context.Background())
	_ = service.stageAllUnreferenced(context.Background())
	service.wait.Add(1)
	go service.loop()
	service.Signal()
}

func (service *Service) recoverInterruptedJobs(ctx context.Context) error {
	now := service.now().UnixMilli()
	_, err := service.database.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',worker_id=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,available_at_ms=?,version=version+1,updated_at_ms=?
WHERE kind IN ('PAYLOAD_RELEASE','BLOB_GC') AND state='RUNNING'
	`, now, now)
	if err != nil {
		return fmt.Errorf("payloadrelease/recover interrupted: %w", err)
	}
	return nil
}

func (service *Service) Close() {
	service.closeOnce.Do(func() { close(service.stop) })
	service.wait.Wait()
}

func (service *Service) Signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *Service) ReconcileGC(ctx context.Context) error {
	if err := service.releaseExpiredProviderPayloads(ctx); err != nil {
		return err
	}
	return service.stageAllUnreferenced(ctx)
}

func (service *Service) loop() {
	defer service.wait.Done()
	poll := time.NewTicker(250 * time.Millisecond)
	maintenance := time.NewTicker(time.Hour)
	defer poll.Stop()
	defer maintenance.Stop()
	for {
		select {
		case <-service.stop:
			return
		case <-service.wake:
		case <-poll.C:
		case <-maintenance.C:
			_ = service.releaseExpiredProviderPayloads(context.Background())
			_ = service.stageAllUnreferenced(context.Background())
		}
		for {
			didWork, _ := service.RunOnce(context.Background())
			if !didWork {
				break
			}
		}
	}
}

// RunOnce is deterministic for integration tests and processes at most one Job.
func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	job, found, err := service.claim(ctx)
	if err != nil || !found {
		return found, err
	}
	executionContext, cancel := context.WithTimeout(ctx, executionTimeout)
	err = service.execute(executionContext, job)
	cancel()
	if finishErr := service.finish(ctx, job, err); finishErr != nil {
		return true, finishErr
	}
	return true, err
}

func (service *Service) claim(ctx context.Context) (claimedJob, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return claimedJob{}, false, fmt.Errorf("payloadrelease/claim: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var job claimedJob
	var inputJSON string
	now := service.now().UnixMilli()
	err = transaction.QueryRowContext(ctx, `
SELECT job.id,job.scope_type,job.scope_id,job.attempt_count,input.input_json
FROM jobs job JOIN job_input_snapshots input ON input.job_id=job.id AND input.execution_no=job.execution_no
WHERE job.kind IN ('PAYLOAD_RELEASE','BLOB_GC') AND job.state='QUEUED' AND job.available_at_ms<=?
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1
`, now).Scan(&job.ID, &job.ScopeType, &job.ScopeID, &job.Attempt, &inputJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return claimedJob{}, false, nil
	}
	if err != nil {
		return claimedJob{}, false, fmt.Errorf("payloadrelease/claim query: %w", err)
	}
	if err := json.Unmarshal([]byte(inputJSON), &job.Input); err != nil {
		return claimedJob{}, false, fmt.Errorf("payloadrelease/claim input: %w", err)
	}
	job.Attempt++
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=?,worker_id='payload-release',execution_started_at_ms=?,
execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, job.Attempt, now, now+executionTimeout.Milliseconds(), now+executionTimeout.Milliseconds(), now, now, job.ID)
	if err != nil {
		return claimedJob{}, false, fmt.Errorf("payloadrelease/claim update: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return claimedJob{}, false, nil
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,?,?,'STARTED',json_object('schemaVersion',1,'executionNo',1,'attempt',?),?)
`, job.ID, job.ScopeType, job.ScopeID, job.Attempt, now); err != nil {
		return claimedJob{}, false, fmt.Errorf("payloadrelease/claim event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return claimedJob{}, false, fmt.Errorf("payloadrelease/claim commit: %w", err)
	}
	return job, true, nil
}

func (service *Service) execute(ctx context.Context, job claimedJob) error {
	if job.Input.SchemaVersion != 1 || job.Input.Scope.ID != job.ScopeID || job.Input.Scope.Type != job.ScopeType {
		return releaseFailure("PAYLOAD_RELEASE_DATABASE_FAILED")
	}
	if job.Input.Kind == "BLOB_GC" {
		return service.executeBlobGC(ctx, job)
	}
	if job.Input.Kind != "PAYLOAD_RELEASE" {
		return releaseFailure("PAYLOAD_RELEASE_DATABASE_FAILED")
	}
	switch job.ScopeType {
	case ScopeImportItem:
		return service.releaseImportItem(ctx, job)
	case ScopeImportJob:
		return service.releaseImportJob(ctx, job)
	case ScopePegasusImportItem:
		return service.releasePegasusItem(ctx, job)
	case ScopeEmulationStationImportItem:
		return service.releaseEmulationStationItem(ctx, job)
	case ScopeUploadConsumption:
		return service.releaseConsumption(ctx, job)
	case ScopeGame:
		return service.releaseGame(ctx, job)
	case ScopeBlob:
		return releaseFailure("PAYLOAD_RELEASE_DATABASE_FAILED")
	default:
		return releaseFailure("PAYLOAD_RELEASE_DATABASE_FAILED")
	}
}
