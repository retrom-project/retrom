package serverimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/firmware"
	"retrom/internal/importing"
)

var (
	errCancelled         = errors.New("server import cancelled")
	errClaimConflict     = errors.New("server import claim conflict")
	errSourceChanged     = errors.New("server import source changed")
	errExecutionDeadline = errors.New("server import execution deadline exceeded")
)

type work struct {
	ImportID        string
	JobID           string
	RootID          string
	RelativePath    string
	RootDigest      string
	CatalogDigest   string
	ReplaceIfBetter bool
	DeadlineAtMS    int64
}

type evaluatedCandidate struct {
	ID                 string
	Item               catalogItem
	File               discoveredFile
	Association        string
	Metadata           blobstore.Metadata
	ArchiveEntries     []importing.ArchiveEntry
	Static             *firmware.StaticEvaluation
	DAT                *firmware.DATEvaluation
	ExpectedDATEntries []firmware.ExpectedDATEntry
	State              string
	Details            map[string]any
}

func (service *Service) runLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-service.stop:
			return
		case <-service.wake:
		case <-ticker.C:
		}
		for {
			if unit, ok := service.claimCancellation(context.Background()); ok {
				service.cancelTask(context.Background(), unit)
				continue
			}
			if unit, ok := service.exhaustedStaleWork(context.Background()); ok {
				service.failTask(context.Background(), unit, "INTERNAL_ERROR")
				continue
			}
			workUnit, ok, err := service.claim(context.Background())
			if err != nil || !ok {
				break
			}
			service.execute(context.Background(), workUnit)
		}
	}
}

// Lease claim SQL is kept contiguous so its compare-and-set predicate remains auditable.
func (service *Service) claim(ctx context.Context) (work, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return work{}, false, fmt.Errorf("begin server import claim: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var unit work
	var replace int
	var jobState string
	var deadline sql.NullInt64
	now := service.now().UnixMilli()
	err = transaction.QueryRowContext(ctx, `
SELECT import.id,import.job_id,import.root_id,import.source_relative_path,import.root_config_digest,
import.catalog_snapshot_digest,import.replace_if_better,job.execution_deadline_at_ms,job.state
FROM server_imports import JOIN jobs job ON job.id=import.job_id
WHERE import.state IN ('QUEUED','RUNNING') AND job.attempt_count<job.max_attempts AND (
 (job.state='QUEUED' AND job.available_at_ms<=?) OR
 (job.state='RUNNING' AND job.leased_until_ms IS NOT NULL AND job.leased_until_ms<=?)
)
ORDER BY import.created_at_ms,import.id LIMIT 1
`, now, now).Scan(
		&unit.ImportID, &unit.JobID, &unit.RootID, &unit.RelativePath, &unit.RootDigest,
		&unit.CatalogDigest, &replace, &deadline, &jobState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return work{}, false, nil
	}
	if err != nil {
		return work{}, false, fmt.Errorf("scan server import claim: %w", err)
	}
	unit.ReplaceIfBetter = replace == 1
	unit.DeadlineAtMS = now + int64(8*time.Hour/time.Millisecond)
	if deadline.Valid {
		unit.DeadlineAtMS = deadline.Int64
	}
	changed, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,
worker_id='server-import-worker',
version=version+1,updated_at_ms=? WHERE id=? AND attempt_count<max_attempts AND (
 (state='QUEUED' AND available_at_ms<=?) OR (state='RUNNING' AND leased_until_ms IS NOT NULL AND leased_until_ms<=?)
)
`, now, unit.DeadlineAtMS, now+60000, now, now, unit.JobID, now, now)
	if err != nil {
		return work{}, false, fmt.Errorf("claim job: %w", err)
	}
	if rows, rowsErr := changed.RowsAffected(); rowsErr != nil {
		return work{}, false, fmt.Errorf("read claimed job row count: %w", rowsErr)
	} else if rows != 1 {
		return work{}, false, fmt.Errorf("claim job changed %d rows: %w", rows, errClaimConflict)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='RUNNING',phase=CASE WHEN state='QUEUED' THEN 'PREPARING_ROOT' ELSE phase END,
version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('QUEUED','RUNNING')
`, now, unit.ImportID); err != nil {
		return work{}, false, fmt.Errorf("claim import: %w", err)
	}
	if jobState == "RUNNING" {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'RETRY_SCHEDULED',json_object('schemaVersion',1,'reason','LEASE_EXPIRED'),?)
`, unit.JobID, unit.ImportID, now); err != nil {
			return work{}, false, fmt.Errorf("claim retry event: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'STARTED','{"schemaVersion":1,"phase":"PREPARING_ROOT"}',?)
`, unit.JobID, unit.ImportID, now); err != nil {
		return work{}, false, fmt.Errorf("claim event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return work{}, false, fmt.Errorf("commit server import claim: %w", err)
	}
	return unit, true, nil
}

func (service *Service) claimCancellation(ctx context.Context) (work, bool) {
	var unit work
	err := service.database.QueryRowContext(ctx, `
SELECT import.id,import.job_id FROM server_imports import
JOIN jobs job ON job.id=import.job_id
WHERE import.state='CANCEL_REQUESTED' AND job.state='CANCEL_REQUESTED'
ORDER BY import.updated_at_ms,import.id LIMIT 1
`).Scan(&unit.ImportID, &unit.JobID)
	return unit, err == nil
}

func (service *Service) exhaustedStaleWork(ctx context.Context) (work, bool) {
	var unit work
	now := service.now().UnixMilli()
	err := service.database.QueryRowContext(ctx, `
SELECT import.id,import.job_id FROM server_imports import
JOIN jobs job ON job.id=import.job_id
WHERE import.state='RUNNING' AND job.state='RUNNING' AND job.attempt_count>=job.max_attempts
AND job.leased_until_ms IS NOT NULL AND job.leased_until_ms<=?
ORDER BY import.updated_at_ms,import.id LIMIT 1
`, now).Scan(&unit.ImportID, &unit.JobID)
	return unit, err == nil
}

// Discovery, cancellation and item commits are one state machine.
func (service *Service) execute(ctx context.Context, unit work) {
	if unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= service.now().UnixMilli() {
		service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
		return
	}
	if unit.DeadlineAtMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(unit.DeadlineAtMS))
		defer cancel()
	}
	heartbeatDone := make(chan struct{})
	go service.heartbeatLoop(ctx, unit, heartbeatDone)
	defer close(heartbeatDone)
	root, exists := service.roots[unit.RootID]
	if !exists || root.digest != unit.RootDigest {
		service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_CHANGED")
		return
	}
	directory, err := openSelectedDirectory(root.path, unit.RelativePath)
	if err != nil {
		service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_UNAVAILABLE")
		return
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	items, err := service.loadItems(ctx, unit.ImportID)
	if err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return
	}
	byRequirement, ok := service.executeDiscovery(ctx, unit, directory, items)
	if !ok {
		return
	}
	if service.cancelRequested(ctx, unit.JobID) {
		service.cancelTask(ctx, unit)
		return
	}
	if !service.installCandidates(ctx, unit, root, items, byRequirement) {
		return
	}
	service.finishTask(ctx, unit)
}

func (service *Service) executeDiscovery(
	ctx context.Context,
	unit work,
	directory *os.File,
	items []catalogItem,
) (map[string][]*evaluatedCandidate, bool) {
	resume, err := service.discoveryWasPersisted(ctx, unit.ImportID)
	if err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return nil, false
	}
	if resume {
		candidates, err := service.loadPersistedCandidates(ctx, unit.ImportID, items)
		if err != nil {
			service.failTask(ctx, unit, "INTERNAL_ERROR")
			return nil, false
		}
		return candidates, true
	}
	_ = service.clearEvaluation(ctx, unit.ImportID)
	service.progress(ctx, unit, "DISCOVERING", 0, int64(len(items)))
	byRequirement, counts, err := service.discoverCandidates(ctx, unit, directory, items)
	if err != nil {
		_ = service.clearEvaluation(ctx, unit.ImportID)
		service.failDiscovery(ctx, unit, err)
		return nil, false
	}
	for _, candidates := range byRequirement {
		markDuplicateBytes(candidates)
	}
	if err := service.persistCandidates(ctx, unit, byRequirement, counts); err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return nil, false
	}
	service.progress(ctx, unit, "DISCOVERY_COMPLETED", int64(len(items)), int64(len(items)))
	return byRequirement, true
}

func (service *Service) failDiscovery(ctx context.Context, unit work, err error) {
	switch {
	case errors.Is(err, errCancelled):
		service.cancelTask(ctx, unit)
	case errors.Is(err, ErrScanLimit):
		service.failTask(ctx, unit, "SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")
	case errors.Is(err, errExecutionDeadline):
		service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
	default:
		service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_UNAVAILABLE")
	}
}

func (service *Service) installCandidates(
	ctx context.Context,
	unit work,
	root Root,
	items []catalogItem,
	byRequirement map[string][]*evaluatedCandidate,
) bool {
	service.progress(ctx, unit, "INSTALLING", 0, int64(len(items)))
	for index, item := range items {
		if item.State != "PENDING" && item.State != "EVALUATING" {
			service.progress(ctx, unit, "INSTALLING", int64(index+1), int64(len(items)))
			continue
		}
		if service.cancelRequested(ctx, unit.JobID) {
			service.cancelTask(ctx, unit)
			return false
		}
		if !service.installCandidate(ctx, unit, root, item, byRequirement[item.RequirementID]) {
			return false
		}
		service.progress(ctx, unit, "INSTALLING", int64(index+1), int64(len(items)))
	}
	return true
}

func (service *Service) installCandidate(
	ctx context.Context,
	unit work,
	root Root,
	item catalogItem,
	candidates []*evaluatedCandidate,
) bool {
	if len(candidates) == 0 {
		service.completeItem(ctx, unit, item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
		return true
	}
	eligible := rankCandidates(candidates)
	if len(eligible) == 0 {
		service.completeItem(ctx, unit, item.RequirementID, "READ_FAILED", nil, "SERVER_IMPORT_SOURCE_UNREADABLE")
		return true
	}
	selected, err := service.verifySelected(ctx, unit, root, eligible[0])
	if errors.Is(err, errCancelled) {
		service.cancelTask(ctx, unit)
		return false
	}
	if errors.Is(err, errExecutionDeadline) {
		service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
		return false
	}
	if err != nil {
		service.completeItem(ctx, unit, item.RequirementID, "SOURCE_CHANGED", eligible[0],
			"SERVER_IMPORT_SOURCE_CHANGED")
		return true
	}
	service.commitCandidate(ctx, unit, item, selected)
	return true
}

func (service *Service) commitCandidate(
	ctx context.Context,
	unit work,
	item catalogItem,
	selected *evaluatedCandidate,
) {
	status, method := selectedStatus(selected)
	_, err := service.firmware.InstallServerCandidate(ctx, firmware.ServerInstallRequest{
		ServerImportID: unit.ImportID, JobID: unit.JobID,
		CandidateID: selected.ID, RequirementID: item.RequirementID, RequirementVersion: item.RequirementVersion,
		ProviderID: item.ProviderID, TargetID: item.TargetID,
		TargetContractSHA256: item.TargetContractSHA256, SourceVersion: item.SourceVersion,
		CatalogDigest: item.CatalogDigest, SourceKind: item.SourceKind, LogicalName: item.LogicalName,
		OriginalFilename: selected.File.Basename,
		Metadata:         selected.Metadata, Status: status, MatchMethod: method, Details: selected.Details,
		ArchiveEntries: selected.ArchiveEntries, ReplaceIfBetter: unit.ReplaceIfBetter,
		StaticExpectation: staticExpectation(item), StaticEvaluation: selected.Static,
		DATExpectedEntries: selected.ExpectedDATEntries, DATEvaluation: selected.DAT,
	})
	switch {
	case errors.Is(err, firmware.ErrCatalogChanged):
		service.completeItem(ctx, unit, item.RequirementID, "CATALOG_CHANGED", selected,
			"BIOS_REQUIREMENT_CATALOG_CHANGED")
	case err != nil:
		service.completeItem(ctx, unit, item.RequirementID, "COMMIT_FAILED", selected, "INTERNAL_ERROR")
	}
}

func (service *Service) heartbeatLoop(ctx context.Context, unit work, done <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-service.stop:
			return
		case <-ticker.C:
			now := service.now().UnixMilli()
			_, _ = service.database.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id='server-import-worker'
`, now, now+60000, now, unit.JobID)
		}
	}
}
