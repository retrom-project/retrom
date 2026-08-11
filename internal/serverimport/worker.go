package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

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

//nolint:lll // Lease claim SQL is kept contiguous so its compare-and-set predicate remains auditable.
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
`, now, now).Scan(&unit.ImportID, &unit.JobID, &unit.RootID, &unit.RelativePath, &unit.RootDigest, &unit.CatalogDigest, &replace, &deadline, &jobState)
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
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,worker_id='server-import-worker',
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

//nolint:funlen,gocognit,gocyclo,nestif // Discovery, cancellation and item commits are one state machine.
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
	var byRequirement map[string][]*evaluatedCandidate
	resumeDiscovery, err := service.discoveryWasPersisted(ctx, unit.ImportID)
	if err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return
	}
	if resumeDiscovery {
		byRequirement, err = service.loadPersistedCandidates(ctx, unit.ImportID, items)
		if err != nil {
			service.failTask(ctx, unit, "INTERNAL_ERROR")
			return
		}
	} else {
		_ = service.clearEvaluation(ctx, unit.ImportID)
		service.progress(ctx, unit, "DISCOVERING", 0, int64(len(items)))
		var counts walkCounts
		var scanErr error
		byRequirement, counts, scanErr = service.discoverCandidates(ctx, unit, directory, items)
		if scanErr != nil {
			_ = service.clearEvaluation(ctx, unit.ImportID)
			switch {
			case errors.Is(scanErr, errCancelled):
				service.cancelTask(ctx, unit)
			case errors.Is(scanErr, ErrScanLimit):
				service.failTask(ctx, unit, "SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")
			case errors.Is(scanErr, errExecutionDeadline):
				service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
			default:
				service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_UNAVAILABLE")
			}
			return
		}
		for _, candidates := range byRequirement {
			markDuplicateBytes(candidates)
		}
		if err := service.persistCandidates(ctx, unit, byRequirement, counts); err != nil {
			service.failTask(ctx, unit, "INTERNAL_ERROR")
			return
		}
		service.progress(ctx, unit, "DISCOVERY_COMPLETED", int64(len(items)), int64(len(items)))
	}
	if service.cancelRequested(ctx, unit.JobID) {
		service.cancelTask(ctx, unit)
		return
	}
	service.progress(ctx, unit, "INSTALLING", 0, int64(len(items)))
	for index, item := range items {
		if item.State != "PENDING" && item.State != "EVALUATING" {
			service.progress(ctx, unit, "INSTALLING", int64(index+1), int64(len(items)))
			continue
		}
		if service.cancelRequested(ctx, unit.JobID) {
			service.cancelTask(ctx, unit)
			return
		}
		candidates := byRequirement[item.RequirementID]
		if len(candidates) == 0 {
			service.completeItem(ctx, unit, item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
			continue
		}
		eligible := rankCandidates(candidates)
		if len(eligible) == 0 {
			service.completeItem(ctx, unit, item.RequirementID, "READ_FAILED", nil, "SERVER_IMPORT_SOURCE_UNREADABLE")
			continue
		}
		selected := eligible[0]
		verified, verifyErr := service.verifySelected(ctx, unit, root, selected)
		if errors.Is(verifyErr, errCancelled) {
			service.cancelTask(ctx, unit)
			return
		}
		if errors.Is(verifyErr, errExecutionDeadline) {
			service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
			return
		}
		if verifyErr != nil {
			service.completeItem(ctx, unit, item.RequirementID, "SOURCE_CHANGED", selected, "SERVER_IMPORT_SOURCE_CHANGED")
			continue
		}
		selected = verified
		status, method := selectedStatus(selected)
		result, installErr := service.firmware.InstallServerCandidate(ctx, firmware.ServerInstallRequest{
			ServerImportID: unit.ImportID, JobID: unit.JobID,
			CandidateID: selected.ID, RequirementID: item.RequirementID, RequirementVersion: item.RequirementVersion,
			CoreArtifactVersion: item.CoreArtifactVersion, SourceVersion: item.SourceVersion,
			CatalogDigest: item.CatalogDigest, SourceKind: item.SourceKind, LogicalName: item.LogicalName,
			OriginalFilename: selected.File.Basename,
			Metadata:         selected.Metadata, Status: status, MatchMethod: method, Details: selected.Details,
			ArchiveEntries: selected.ArchiveEntries, ReplaceIfBetter: unit.ReplaceIfBetter,
			StaticExpectation: staticExpectation(item), StaticEvaluation: selected.Static,
			DATExpectedEntries: selected.ExpectedDATEntries, DATEvaluation: selected.DAT,
		})
		switch {
		case errors.Is(installErr, firmware.ErrCatalogChanged):
			service.completeItem(ctx, unit, item.RequirementID, "CATALOG_CHANGED", selected, "BIOS_REQUIREMENT_CATALOG_CHANGED")
		case installErr != nil:
			service.completeItem(ctx, unit, item.RequirementID, "COMMIT_FAILED", selected, "INTERNAL_ERROR")
		default:
			item.State = result.Outcome
		}
		service.progress(ctx, unit, "INSTALLING", int64(index+1), int64(len(items)))
	}
	service.finishTask(ctx, unit)
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

type association struct {
	item catalogItem
	kind string
}

type candidateHashTask struct {
	file         discoveredFile
	associations []association
}

type candidateHashResult struct {
	candidates  []*evaluatedCandidate
	hashedBytes int64
	err         error
}

//nolint:funlen,gocognit,gocyclo // Bounded walker, hash pool and archive evaluation coordinate one discovery barrier.
func (service *Service) discoverCandidates(
	ctx context.Context,
	unit work,
	directory *os.File,
	items []catalogItem,
) (map[string][]*evaluatedCandidate, walkCounts, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	index := newCandidateIndex(items)
	tasks := make(chan candidateHashTask, 4)
	results := make(chan candidateHashResult, 4)
	var workers sync.WaitGroup
	for range service.scanLimits.hashWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range tasks {
				result := service.hashDiscoveredCandidate(ctx, unit, task)
				cleanup.Error("close", task.file.Parent.Close())
				results <- result
			}
		}()
	}
	go func() {
		workers.Wait()
		close(results)
	}()
	type walkResult struct {
		counts walkCounts
		err    error
	}
	walkDone := make(chan walkResult, 1)
	go func() {
		var physicalCandidates int64
		var hashPhase sync.Once
		counts, err := walkFiles(directory, service.scanLimits, func(file discoveredFile) error {
			if service.cancelRequested(ctx, unit.JobID) {
				return errCancelled
			}
			matched := index.associations(file)
			if len(matched) == 0 {
				return nil
			}
			physicalCandidates++
			if physicalCandidates > service.scanLimits.maxPhysicalCandidates {
				return ErrScanLimit
			}
			parent, duplicateErr := duplicateDirectory(file.Parent)
			if duplicateErr != nil {
				return ErrRootUnavailable
			}
			file.Parent = parent
			hashPhase.Do(func() { service.progress(ctx, unit, "HASHING", 0, physicalCandidates) })
			select {
			case tasks <- candidateHashTask{file: file, associations: matched}:
				return nil
			case <-ctx.Done():
				cleanup.Error("close", parent.Close())
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return errExecutionDeadline
				}
				return ctx.Err()
			}
		})
		close(tasks)
		walkDone <- walkResult{counts: counts, err: err}
	}()

	grouped := make(map[string][]*evaluatedCandidate)
	var firstErr error
	var hashedBytes int64
	for result := range results {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			cancel()
		}
		hashedBytes += result.hashedBytes
		if hashedBytes > service.scanLimits.maxHashedBytes && firstErr == nil {
			firstErr = ErrScanLimit
			cancel()
		}
		for _, candidate := range result.candidates {
			values := grouped[candidate.Item.RequirementID]
			values = append(values, candidate)
			grouped[candidate.Item.RequirementID] = values
			if len(values) > service.scanLimits.maxCandidatesPerRequirement && firstErr == nil {
				firstErr = ErrScanLimit
				cancel()
			}
		}
	}
	walked := <-walkDone
	if walked.err != nil && firstErr == nil {
		firstErr = walked.err
	}
	return grouped, walked.counts, firstErr
}

//nolint:gocyclo,lll // Read, re-stat and per-association evaluation branches are independent source-safety checks.
func (service *Service) hashDiscoveredCandidate(
	ctx context.Context,
	unit work,
	task candidateHashTask,
) candidateHashResult {
	result := candidateHashResult{}
	handle, before, openErr := openCandidate(task.file)
	if openErr != nil {
		for _, association := range task.associations {
			result.candidates = append(result.candidates, failedCandidate(association.item, task.file, association.kind, "READ_FAILED"))
		}
		return result
	}
	metadata, putErr := service.blobs.Put(&cancelReader{
		ctx: ctx, reader: handle, check: func() bool { return service.pollCancellation(ctx, unit) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if errors.Is(putErr, errCancelled) || errors.Is(putErr, errExecutionDeadline) ||
		errors.Is(putErr, context.Canceled) || errors.Is(putErr, context.DeadlineExceeded) {
		result.err = putErr
		return result
	}
	if putErr != nil || statErr != nil || !sameFileFacts(before, after) {
		for _, association := range task.associations {
			result.candidates = append(result.candidates, failedCandidate(association.item, task.file, association.kind, "SOURCE_CHANGED"))
		}
		return result
	}
	result.hashedBytes = metadata.Size
	facts := firmware.FileFacts{
		RelativePath: task.file.RelativePath, Basename: task.file.Basename, SizeBytes: metadata.Size,
		MD5: metadata.MD5, SHA1: metadata.SHA1, SHA256: metadata.SHA256, CRC32: metadata.CRC32,
	}
	for _, association := range task.associations {
		candidate, evaluateErr := service.evaluate(ctx, association.item, association.kind, task.file, metadata, facts)
		if evaluateErr != nil {
			if errors.Is(evaluateErr, errExecutionDeadline) || errors.Is(evaluateErr, context.Canceled) ||
				errors.Is(evaluateErr, context.DeadlineExceeded) {
				result.err = evaluateErr
				return result
			}
			candidate = failedCandidate(association.item, task.file, association.kind, "ARCHIVE_UNSAFE")
			candidate.Metadata = metadata
		}
		if association.kind == "RENAMED_HASH_MATCH" && (candidate.Static == nil || !candidate.Static.ExactHash) {
			continue
		}
		result.candidates = append(result.candidates, candidate)
	}
	return result
}

type candidateIndex struct {
	byName map[string][]catalogItem
	bySize map[int64][]catalogItem
}

func newCandidateIndex(items []catalogItem) candidateIndex {
	result := candidateIndex{byName: make(map[string][]catalogItem), bySize: make(map[int64][]catalogItem)}
	for _, item := range items {
		if item.SourceKind != "DAT_MACHINE" || strings.HasSuffix(importing.ASCIICaseFold(item.LogicalName), ".zip") {
			key := importing.ASCIICaseFold(item.LogicalName)
			result.byName[key] = append(result.byName[key], item)
		}
		if item.SourceKind == "STATIC" && item.ExpectedSize != nil &&
			(item.ExpectedMD5 != nil || item.ExpectedSHA1 != nil || item.ExpectedSHA256 != nil) {
			result.bySize[*item.ExpectedSize] = append(result.bySize[*item.ExpectedSize], item)
		}
	}
	return result
}

func (index candidateIndex) associations(file discoveredFile) []association {
	result := make([]association, 0)
	folded := importing.ASCIICaseFold(file.Basename)
	for _, item := range index.byName[folded] {
		if item.SourceKind != "DAT_MACHINE" || strings.HasSuffix(folded, ".zip") {
			kind := "CASEFOLD_NAME"
			if file.Basename == item.LogicalName {
				kind = "EXACT_NAME"
			}
			result = append(result, association{item: item, kind: kind})
		}
	}
	for _, item := range index.bySize[file.SizeBytes] {
		if folded != importing.ASCIICaseFold(item.LogicalName) {
			result = append(result, association{item: item, kind: "RENAMED_HASH_MATCH"})
		}
	}
	return result
}

//nolint:lll // Evaluation evidence keys deliberately match the persisted/API detail contract.
func (service *Service) evaluate(ctx context.Context, item catalogItem, association string, file discoveredFile,
	metadata blobstore.Metadata, facts firmware.FileFacts,
) (*evaluatedCandidate, error) {
	id, _ := uuid.NewV7()
	candidate := &evaluatedCandidate{ID: id.String(), Item: item, File: file, Association: association, Metadata: metadata, State: "ELIGIBLE"}
	if item.SourceKind == "STATIC" {
		expectation := firmware.StaticExpectation{LogicalName: item.LogicalName, SizeBytes: item.ExpectedSize}
		if item.ExpectedMD5 != nil {
			expectation.MD5 = *item.ExpectedMD5
		}
		if item.ExpectedSHA1 != nil {
			expectation.SHA1 = *item.ExpectedSHA1
		}
		if item.ExpectedSHA256 != nil {
			expectation.SHA256 = *item.ExpectedSHA256
		}
		evaluation := firmware.EvaluateStatic(expectation, facts)
		candidate.Static = &evaluation
		candidate.Details = map[string]any{"schemaVersion": 1, "exactHash": evaluation.ExactHash, "expectedSizeMatched": evaluation.ExpectedSizeMatched, "exactBasename": evaluation.ExactBasename}
		return candidate, nil
	}
	select {
	case service.archiveScan <- struct{}{}:
		defer func() { <-service.archiveScan }()
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return candidate, errExecutionDeadline
		}
		return candidate, fmt.Errorf("wait for archive scan slot: %w", ctx.Err())
	}
	entries, err := importing.ScanZIP(ctx, metadata.Path, importing.DefaultArchiveLimits())
	if err != nil {
		return candidate, fmt.Errorf("scan server import ZIP candidate: %w", err)
	}
	expected, err := service.expectedDATEntries(ctx, item)
	if err != nil {
		return candidate, err
	}
	evaluation := firmware.EvaluateDAT(item.LogicalName, expected, facts, entries)
	candidate.DAT = &evaluation
	candidate.ExpectedDATEntries = expected
	candidate.ArchiveEntries = entries
	candidate.Details = map[string]any{"schemaVersion": 1, "launchable": evaluation.Launchable, "matchedCount": evaluation.MatchedCount, "aliasedCount": evaluation.AliasedCount, "mismatchedCount": evaluation.MismatchedCount, "missingCount": evaluation.MissingCount, "extraCount": evaluation.ExtraCount, "exactBasename": evaluation.ExactBasename}
	return candidate, nil
}

func staticExpectation(item catalogItem) *firmware.StaticExpectation {
	if item.SourceKind != "STATIC" {
		return nil
	}
	result := &firmware.StaticExpectation{LogicalName: item.LogicalName, SizeBytes: item.ExpectedSize}
	if item.ExpectedMD5 != nil {
		result.MD5 = *item.ExpectedMD5
	}
	if item.ExpectedSHA1 != nil {
		result.SHA1 = *item.ExpectedSHA1
	}
	if item.ExpectedSHA256 != nil {
		result.SHA256 = *item.ExpectedSHA256
	}
	return result
}

func markDuplicateBytes(candidates []*evaluatedCandidate) {
	ranked := rankCandidates(candidates)
	seen := make(map[string]struct{}, len(ranked))
	for _, candidate := range ranked {
		if candidate.Metadata.SHA256 == "" {
			continue
		}
		if _, exists := seen[candidate.Metadata.SHA256]; exists {
			candidate.State = "DUPLICATE_BYTES"
			candidate.Details["code"] = "BIOS_CANDIDATE_DUPLICATE_BYTES"
			continue
		}
		seen[candidate.Metadata.SHA256] = struct{}{}
	}
}

func sameMetadata(left, right blobstore.Metadata) bool {
	return left.Size == right.Size && left.MD5 == right.MD5 && left.SHA1 == right.SHA1 &&
		left.SHA256 == right.SHA256 && left.CRC32 == right.CRC32
}

func (service *Service) verifySelected(
	ctx context.Context,
	unit work,
	root Root,
	selected *evaluatedCandidate,
) (*evaluatedCandidate, error) {
	handle, before, err := openRelativeCandidate(root.path, unit.RelativePath, selected.File.RelativePath)
	if err != nil {
		return nil, errSourceChanged
	}
	metadata, putErr := service.blobs.Put(&cancelReader{
		ctx: ctx, reader: handle, check: func() bool { return service.pollCancellation(ctx, unit) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if errors.Is(putErr, errCancelled) {
		return nil, errCancelled
	}
	if putErr != nil || statErr != nil || !sameFileFacts(before, after) || !sameMetadata(metadata, selected.Metadata) {
		return nil, errSourceChanged
	}
	facts := firmware.FileFacts{
		RelativePath: selected.File.RelativePath, Basename: selected.File.Basename, SizeBytes: metadata.Size,
		MD5: metadata.MD5, SHA1: metadata.SHA1, SHA256: metadata.SHA256, CRC32: metadata.CRC32,
	}
	verified, err := service.evaluate(ctx, selected.Item, selected.Association, selected.File, metadata, facts)
	if err != nil || verified.State != "ELIGIBLE" {
		return nil, errSourceChanged
	}
	verified.ID = selected.ID
	if selected.Static != nil {
		if verified.Static == nil || firmware.CompareStatic(*verified.Static, *selected.Static) != 0 {
			return nil, errSourceChanged
		}
	} else if verified.DAT == nil || firmware.CompareDAT(*verified.DAT, *selected.DAT) != 0 {
		return nil, errSourceChanged
	}
	return verified, nil
}

func failedCandidate(item catalogItem, file discoveredFile, association, state string) *evaluatedCandidate {
	id, _ := uuid.NewV7()
	code := "SERVER_IMPORT_SOURCE_UNREADABLE"
	switch state {
	case "SOURCE_CHANGED":
		code = "SERVER_IMPORT_SOURCE_CHANGED"
	case "ARCHIVE_UNSAFE":
		code = "ARCHIVE_UNSAFE"
	}
	return &evaluatedCandidate{
		ID: id.String(), Item: item, File: file, Association: association, State: state,
		Details: map[string]any{"schemaVersion": 1, "code": code},
	}
}

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
	check  func() bool
	since  int64
}

//nolint:wrapcheck // io.Reader must return the underlying io.EOF sentinel unchanged to satisfy io.Copy.
func (reader *cancelReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, errExecutionDeadline
		}
		return 0, fmt.Errorf("candidate read context: %w", err)
	}
	if reader.since >= 8<<20 {
		reader.since = 0
		if reader.check != nil && reader.check() {
			return 0, errCancelled
		}
	}
	remaining := int64(8<<20) - reader.since
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := reader.reader.Read(buffer)
	reader.since += int64(count)
	return count, err
}

func (service *Service) expectedDATEntries(ctx context.Context, item catalogItem) ([]firmware.ExpectedDATEntry, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT entry.name,entry.size_bytes,entry.crc32,entry.sha1
FROM dat_rom_entries entry
WHERE entry.dat_version_id=? AND entry.machine_name=? AND COALESCE(entry.status,'GOOD')<>'NODUMP'
AND (entry.bios_name IS NULL OR EXISTS(
 SELECT 1 FROM dat_bios_sets bios WHERE bios.dat_version_id=entry.dat_version_id
 AND bios.machine_name=entry.machine_name AND bios.bios_name=entry.bios_name AND bios.is_default=1
)) ORDER BY entry.name COLLATE BINARY,entry.ordinal
`, item.DATVersionID, item.DATMachineName)
	if err != nil {
		return nil, fmt.Errorf("query expected DAT entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]firmware.ExpectedDATEntry, 0)
	for rows.Next() {
		var entry firmware.ExpectedDATEntry
		var crc, sha sql.NullString
		if err := rows.Scan(&entry.Name, &entry.SizeBytes, &crc, &sha); err != nil {
			return nil, fmt.Errorf("scan expected DAT entry: %w", err)
		}
		if crc.Valid {
			entry.CRC32 = crc.String
		}
		if sha.Valid {
			entry.SHA1 = sha.String
		}
		result = append(result, entry)
	}
	if len(result) == 0 {
		return nil, ErrCatalogInvalid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expected DAT entries: %w", err)
	}
	return result, nil
}

func rankCandidates(values []*evaluatedCandidate) []*evaluatedCandidate {
	eligible := make([]*evaluatedCandidate, 0, len(values))
	for _, value := range values {
		if value.State == "ELIGIBLE" {
			eligible = append(eligible, value)
		}
	}
	sort.Slice(eligible, func(left, right int) bool {
		if eligible[left].Static != nil {
			return firmware.CompareStatic(*eligible[left].Static, *eligible[right].Static) < 0
		}
		return firmware.CompareDAT(*eligible[left].DAT, *eligible[right].DAT) < 0
	})
	return eligible
}

func selectedStatus(candidate *evaluatedCandidate) (string, string) {
	if candidate.Static != nil {
		return candidate.Static.Status, candidate.Static.Method
	}
	return candidate.DAT.Status, candidate.DAT.Method
}

//nolint:lll // Catalog snapshot projection intentionally mirrors the frozen import-item schema.
func (service *Service) loadItems(ctx context.Context, importID string) ([]catalogItem, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT requirement_id,requirement_version,core_id,core_name_snapshot,core_artifact_id,core_artifact_version,
source_kind,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path,catalog_digest,
activation_options_json,source_version,dat_version_id,dat_machine_name,expected_size_bytes,expected_md5,expected_sha1,expected_sha256,
active_installation_id_snapshot,active_installation_version_snapshot,active_blob_sha256_snapshot,
active_status_snapshot,active_validated_requirement_version_snapshot,state
FROM server_bios_import_items WHERE server_import_id=? ORDER BY requirement_id COLLATE BINARY`, importID)
	if err != nil {
		return nil, fmt.Errorf("query server import items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]catalogItem, 0)
	for rows.Next() {
		var item catalogItem
		if err := rows.Scan(&item.RequirementID, &item.RequirementVersion, &item.CoreID, &item.CoreName, &item.CoreArtifactID, &item.CoreArtifactVersion, &item.SourceKind, &item.LogicalName, &item.RequirementMode, &item.ConditionCode, &item.DeliveryKind, &item.EmulatorPath, &item.CatalogDigest, &item.ActivationOptionsJSON, &item.SourceVersion, &item.DATVersionID, &item.DATMachineName, &item.ExpectedSize, &item.ExpectedMD5, &item.ExpectedSHA1, &item.ExpectedSHA256, &item.ActiveInstallationID, &item.ActiveInstallationVersion, &item.ActiveBlobSHA256, &item.ActiveStatus, &item.ActiveValidatedVersion, &item.State); err != nil {
			return nil, fmt.Errorf("scan server import item: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server import items: %w", err)
	}
	return result, nil
}

//nolint:lll // The phase probe is the persisted discovery-resume boundary.
func (service *Service) discoveryWasPersisted(ctx context.Context, importID string) (bool, error) {
	var phase sql.NullString
	if err := service.database.QueryRowContext(ctx, `SELECT phase FROM server_imports WHERE id=?`, importID).Scan(&phase); err != nil {
		return false, fmt.Errorf("read server import discovery phase: %w", err)
	}
	if !phase.Valid {
		return false, nil
	}
	switch phase.String {
	case "DISCOVERY_COMPLETED", "RANKING", "INSTALLING", "QUEUEING_REVALIDATION":
		return true, nil
	default:
		return false, nil
	}
}

//nolint:funlen,nestif // One row reconstructs the complete candidate evaluation needed for crash recovery.
func (service *Service) loadPersistedCandidates(
	ctx context.Context,
	importID string,
	items []catalogItem,
) (map[string][]*evaluatedCandidate, error) {
	itemByID := make(map[string]catalogItem, len(items))
	for _, item := range items {
		itemByID[item.RequirementID] = item
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT id,requirement_id,relative_path,basename,association_kind,size_bytes,md5,sha1,sha256,crc32,state,
exact_hash,expected_size_match,exact_basename,safe_archive,launchable,matched_count,aliased_count,
mismatched_count,missing_count,extra_count,evaluation_details_json
FROM server_bios_import_candidates WHERE server_import_id=?
ORDER BY requirement_id COLLATE BINARY,COALESCE(rank_ordinal,9223372036854775807),id
`, importID)
	if err != nil {
		return nil, fmt.Errorf("query persisted server import candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make(map[string][]*evaluatedCandidate)
	datExpected := make(map[string][]firmware.ExpectedDATEntry)
	for rows.Next() {
		var candidate evaluatedCandidate
		var requirementID string
		var md5Value, sha1Value, sha256Value, crc32Value, details sql.NullString
		var exactHash, expectedSize, exactName, safeArchive, launchable sql.NullInt64
		var matched, aliased, mismatched, missing, extra sql.NullInt64
		if err := rows.Scan(&candidate.ID, &requirementID, &candidate.File.RelativePath, &candidate.File.Basename,
			&candidate.Association, &candidate.File.SizeBytes, &md5Value, &sha1Value, &sha256Value, &crc32Value,
			&candidate.State, &exactHash, &expectedSize, &exactName, &safeArchive, &launchable, &matched,
			&aliased, &mismatched, &missing, &extra, &details); err != nil {
			return nil, fmt.Errorf("scan persisted server import candidate: %w", err)
		}
		item, ok := itemByID[requirementID]
		if !ok {
			return nil, ErrCatalogInvalid
		}
		candidate.Item = item
		candidate.File.Name = candidate.File.Basename
		candidate.Metadata = blobstore.Metadata{
			Size: candidate.File.SizeBytes,
			MD5:  nullableString(md5Value), SHA1: nullableString(sha1Value),
			SHA256: nullableString(sha256Value), CRC32: nullableString(crc32Value),
		}
		if candidate.Metadata.SHA256 != "" {
			candidate.Metadata.Path = service.blobs.Path(candidate.Metadata.SHA256)
		}
		candidate.Details = map[string]any{}
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &candidate.Details)
		}
		facts := firmware.FileFacts{
			RelativePath: candidate.File.RelativePath, Basename: candidate.File.Basename,
			SizeBytes: candidate.Metadata.Size, MD5: candidate.Metadata.MD5, SHA1: candidate.Metadata.SHA1,
			SHA256: candidate.Metadata.SHA256, CRC32: candidate.Metadata.CRC32,
		}
		if item.SourceKind == "STATIC" && exactHash.Valid {
			candidate.Static = &firmware.StaticEvaluation{
				Facts: facts, ExactHash: exactHash.Int64 == 1, ExpectedSizeMatched: expectedSize.Int64 == 1,
				ExactBasename: exactName.Int64 == 1,
			}
			candidate.Static.Status, candidate.Static.Method = staticStatusMethod(*candidate.Static)
		} else if item.SourceKind == "DAT_MACHINE" && safeArchive.Valid {
			candidate.DAT = &firmware.DATEvaluation{
				Facts: facts, SafeArchive: safeArchive.Int64 == 1, Launchable: launchable.Int64 == 1,
				MatchedCount: int(matched.Int64), AliasedCount: int(aliased.Int64),
				MismatchedCount: int(mismatched.Int64), MissingCount: int(missing.Int64),
				ExtraCount: int(extra.Int64), ExactBasename: exactName.Int64 == 1,
			}
			candidate.DAT.Status, candidate.DAT.Method = datStatusMethod(*candidate.DAT)
			expected, exists := datExpected[requirementID]
			if !exists {
				expected, err = service.expectedDATEntries(ctx, item)
				if err != nil {
					return nil, fmt.Errorf("restore expected DAT entries: %w", err)
				}
				datExpected[requirementID] = expected
			}
			candidate.ExpectedDATEntries = expected
		}
		result[requirementID] = append(result[requirementID], &candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persisted server import candidates: %w", err)
	}
	return result, nil
}

func nullableString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func staticStatusMethod(value firmware.StaticEvaluation) (string, string) {
	if value.ExactHash {
		return "MATCHED", "EXACT_HASH"
	}
	if value.ExpectedSizeMatched {
		return "HASH_WARNING", "EXPECTED_SIZE_FALLBACK"
	}
	return "HASH_WARNING", "LARGEST_SIZE_FALLBACK"
}

func datStatusMethod(value firmware.DATEvaluation) (string, string) {
	if value.MissingCount > 0 {
		return "MISSING_ENTRY", "DAT_PARTIAL_FALLBACK"
	}
	if value.MismatchedCount > 0 {
		return "HASH_WARNING", "DAT_ENTRY_WARNING"
	}
	return "MATCHED", "DAT_ENTRY_MATCH"
}

//nolint:lll // Clearing candidates and their item projections is one resumable-discovery reset.
func (service *Service) clearEvaluation(ctx context.Context, importID string) error {
	if _, err := service.database.ExecContext(ctx, `DELETE FROM server_bios_import_candidates WHERE server_import_id=?`, importID); err != nil {
		return fmt.Errorf("clear server import candidates: %w", err)
	}
	_, err := service.database.ExecContext(ctx, `UPDATE server_bios_import_items SET state='PENDING',candidate_count=0,match_method=NULL,selection_details_json=NULL,
previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,completed_at_ms=NULL,updated_at_ms=?
WHERE server_import_id=? AND state IN ('PENDING','EVALUATING')`, service.now().UnixMilli(), importID)
	if err != nil {
		return fmt.Errorf("reset server import items: %w", err)
	}
	return nil
}

//nolint:funlen,gocognit,gocyclo,lll // Candidate evidence, ranks and discovery completion commit atomically.
func (service *Service) persistCandidates(
	ctx context.Context,
	unit work,
	groups map[string][]*evaluatedCandidate,
	counts walkCounts,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin server import candidate persistence: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	total := 0
	multi := 0
	for requirementID, values := range groups {
		eligible := rankCandidates(values)
		rank := make(map[string]int, len(eligible))
		for index, candidate := range eligible {
			rank[candidate.ID] = index + 1
		}
		if len(values) > 1 {
			multi++
		}
		for _, candidate := range values {
			total++
			details, _ := json.Marshal(candidate.Details)
			var staticExact, staticSize, safe, launchable any
			var matched, aliased, mismatched, missing, extra any
			exactName := candidate.File.Basename == candidate.Item.LogicalName
			if candidate.Static != nil {
				staticExact = boolInteger(candidate.Static.ExactHash)
				staticSize = boolInteger(candidate.Static.ExpectedSizeMatched)
			}
			if candidate.DAT != nil {
				safe = 1
				launchable = boolInteger(candidate.DAT.Launchable)
				matched = candidate.DAT.MatchedCount
				aliased = candidate.DAT.AliasedCount
				mismatched = candidate.DAT.MismatchedCount
				missing = candidate.DAT.MissingCount
				extra = candidate.DAT.ExtraCount
			}
			var rankValue any
			if value := rank[candidate.ID]; value > 0 {
				rankValue = value
			}
			var notSelectedReason any
			switch {
			case candidate.State == "DUPLICATE_BYTES":
				notSelectedReason = "DUPLICATE_BYTES"
			case rank[candidate.ID] > 1:
				notSelectedReason = "LOWER_RANK"
			case candidate.State != "ELIGIBLE":
				if code, ok := candidate.Details["code"].(string); ok {
					notSelectedReason = code
				} else {
					notSelectedReason = "INELIGIBLE"
				}
			}
			var md5Value, sha1Value, sha256Value, crc32Value any
			if candidate.Metadata.SHA256 != "" {
				md5Value = candidate.Metadata.MD5
				sha1Value = candidate.Metadata.SHA1
				sha256Value = candidate.Metadata.SHA256
				crc32Value = candidate.Metadata.CRC32
			}
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO server_bios_import_candidates(id,server_import_id,requirement_id,relative_path,basename,
association_kind,size_bytes,md5,sha1,sha256,crc32,state,exact_hash,expected_size_match,exact_basename,
safe_archive,launchable,matched_count,aliased_count,mismatched_count,missing_count,extra_count,rank_ordinal,
not_selected_reason,evaluation_details_json,created_at_ms,updated_at_ms,evaluated_at_ms)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, candidate.ID, unit.ImportID, requirementID, candidate.File.RelativePath, candidate.File.Basename, candidate.Association, candidate.File.SizeBytes, md5Value, sha1Value, sha256Value, crc32Value, candidate.State, staticExact, staticSize, boolInteger(exactName), safe, launchable, matched, aliased, mismatched, missing, extra, rankValue, notSelectedReason, string(details), now, now, now); err != nil {
				return fmt.Errorf("persist server import candidate: %w", err)
			}
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE server_bios_import_items SET state='EVALUATING',candidate_count=?,updated_at_ms=? WHERE server_import_id=? AND requirement_id=?`, len(values), now, unit.ImportID, requirementID); err != nil {
			return fmt.Errorf("update server import candidate count: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE server_imports SET phase='RANKING',candidate_count=?,evaluated_item_count=catalog_item_count,multi_candidate_item_count=?,skipped_special_count=?,skipped_unrepresentable_path_count=?,version=version+1,updated_at_ms=? WHERE id=?`, total, multi, counts.SkippedSpecial, counts.SkippedUnrepresentable, now, unit.ImportID); err != nil {
		return fmt.Errorf("complete server import discovery: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit server import candidates: %w", err)
	}
	return nil
}

//nolint:lll // Progress updates deliberately mirror import, lease and event state in one helper.
func (service *Service) progress(ctx context.Context, unit work, phase string, current, total int64) {
	now := service.now().UnixMilli()
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "phase": phase, "completed": current, "total": total})
	_, _ = service.database.ExecContext(ctx, `UPDATE server_imports SET phase=?,version=version+1,updated_at_ms=? WHERE id=?`, phase, now, unit.ImportID)
	_, _ = service.database.ExecContext(ctx, `UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=? WHERE id=?`, now, now+60000, now, unit.JobID)
	_, _ = service.database.ExecContext(ctx, `INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)`, unit.JobID, unit.ImportID, string(data), now)
}

//nolint:lll // Item outcome, selected candidate and progress event are committed together.
func (service *Service) completeItem(
	ctx context.Context,
	unit work,
	requirementID, state string,
	candidate *evaluatedCandidate,
	code string,
) {
	now := service.now().UnixMilli()
	var method, details any
	if candidate != nil {
		_, methodValue := selectedStatus(candidate)
		method = methodValue
		encoded, _ := json.Marshal(candidate.Details)
		details = string(encoded)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `UPDATE server_bios_import_items SET state=?,match_method=?,selection_details_json=?,outcome_code=?,previous_installation_id=?,new_installation_id=?,completed_at_ms=?,updated_at_ms=? WHERE server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')`, state, method, details, code, nil, nil, now, now, unit.ImportID, requirementID); err != nil {
		return
	}
	if candidate != nil {
		candidateState := candidate.State
		if state == "SOURCE_CHANGED" {
			candidateState = "SOURCE_CHANGED"
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE server_bios_import_candidates SET state=?,not_selected_reason=?,updated_at_ms=? WHERE id=?`, candidateState, code, now, candidate.ID); err != nil {
			return
		}
	}
	eventJSON, _ := json.Marshal(map[string]any{"schemaVersion": 1, "phase": "INSTALLING", "result": state})
	if _, err := transaction.ExecContext(ctx, `INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)`, unit.JobID, unit.ImportID, string(eventJSON), now); err != nil {
		return
	}
	_ = transaction.Commit()
}

//nolint:lll // This single-row state probe is on the hot cancellation path.
func (service *Service) cancelRequested(ctx context.Context, jobID string) bool {
	var state string
	return service.database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state) == nil && (state == "CANCEL_REQUESTED" || state == "CANCELLED")
}

//nolint:lll // Polling renews the lease before reading cancellation state.
func (service *Service) pollCancellation(ctx context.Context, unit work) bool {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'`, now, now+60000, now, unit.JobID)
	return service.cancelRequested(ctx, unit.JobID)
}

//nolint:lll // Terminal import/job/event state is committed as one transaction.
func (service *Service) finishTask(ctx context.Context, unit work) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil {
		return
	}
	failed := counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] + counts["READ_FAILED"] + counts["COMMIT_FAILED"]
	state := "COMPLETED"
	if failed > 0 {
		state = "PARTIAL_FAILURE"
	}
	if err := updateTerminalImport(ctx, transaction, unit.ImportID, state, "QUEUEING_REVALIDATION", nil, counts, now); err != nil {
		return
	}
	_, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE id=?
`, now, now, now, unit.JobID)
	if err != nil {
		return
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'SUCCEEDED','{"schemaVersion":1}',?)`, unit.JobID, unit.ImportID, now)
	if err == nil {
		_ = transaction.Commit()
	}
}

//nolint:lll // Failure item/import/job/event state is committed as one transaction.
func (service *Service) failTask(ctx context.Context, unit work, code string) {
	now := service.now().UnixMilli()
	retryable := 0
	if code == "SERVER_IMPORT_ROOT_UNAVAILABLE" || code == "INTERNAL_ERROR" {
		retryable = 1
	}
	if retryable == 1 && service.scheduleAutomaticRetry(ctx, unit, code, now) {
		return
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `UPDATE server_bios_import_items SET state='COMMIT_FAILED',outcome_code=?,completed_at_ms=?,updated_at_ms=? WHERE server_import_id=? AND state IN ('PENDING','EVALUATING')`, code, now, now, unit.ImportID); err != nil {
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil || updateTerminalImport(ctx, transaction, unit.ImportID, "FAILED", "", &code, counts, now) != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE jobs SET state='FAILED',error_code=?,error_retryable=?,finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=? WHERE id=?`, code, retryable, now, now, unit.JobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,'SERVER_IMPORT',?,'FAILED',json_object('schemaVersion',1,'code',?),?)`, unit.JobID, unit.ImportID, code, now); err == nil {
		_ = transaction.Commit()
	}
}

//nolint:lll // Retry reset, lease release and retry event must remain one atomic state transition.
func (service *Service) scheduleAutomaticRetry(ctx context.Context, unit work, code string, now int64) bool {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	var attempt, maximum int64
	var deadline sql.NullInt64
	var terminalItems int64
	if err := transaction.QueryRowContext(ctx, `
SELECT job.attempt_count,job.max_attempts,job.execution_deadline_at_ms,
 (SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=import.id
  AND item.state NOT IN ('PENDING','EVALUATING'))
FROM jobs job JOIN server_imports import ON import.job_id=job.id
WHERE job.id=? AND job.state='RUNNING'
`, unit.JobID).Scan(&attempt, &maximum, &deadline, &terminalItems); err != nil ||
		terminalItems != 0 || attempt >= maximum || !deadline.Valid {
		return false
	}
	delays := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 120 * time.Second}
	delayIndex := int(attempt - 1)
	if delayIndex < 0 {
		delayIndex = 0
	}
	if delayIndex >= len(delays) {
		delayIndex = len(delays) - 1
	}
	availableAt := now + delays[delayIndex].Milliseconds()
	if availableAt >= deadline.Int64 {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM server_bios_import_candidates WHERE server_import_id=?`, unit.ImportID); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='PENDING',candidate_count=0,match_method=NULL,
selection_details_json=NULL,previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,
completed_at_ms=NULL,updated_at_ms=? WHERE server_import_id=?
`, now, unit.ImportID); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='QUEUED',phase=NULL,candidate_count=0,evaluated_item_count=0,
multi_candidate_item_count=0,skipped_special_count=0,skipped_unrepresentable_path_count=0,
last_error_code=NULL,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, unit.ImportID); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
error_code=NULL,error_retryable=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, availableAt, now, unit.JobID); err != nil {
		return false
	}
	eventJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attempt": attempt, "retryAtMs": availableAt, "errorCode": code,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'RETRY_SCHEDULED',?,?)
`, unit.JobID, unit.ImportID, string(eventJSON), now); err != nil {
		return false
	}
	if err := transaction.Commit(); err != nil {
		return false
	}
	time.AfterFunc(delays[delayIndex], service.signal)
	return true
}

//nolint:lll // Cancellation item/import/job/event state is committed as one transaction.
func (service *Service) cancelTask(ctx context.Context, unit work) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `UPDATE server_bios_import_items SET state='CANCELLED',outcome_code='CANCELLED',completed_at_ms=?,updated_at_ms=? WHERE server_import_id=? AND state IN ('PENDING','EVALUATING')`, now, now, unit.ImportID); err != nil {
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil || updateTerminalImport(ctx, transaction, unit.ImportID, "CANCELLED", "", nil, counts, now) != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=? WHERE id=?`, now, now, now, unit.JobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,'SERVER_IMPORT',?,'CANCELLED','{"schemaVersion":1}',?)`, unit.JobID, unit.ImportID, now); err == nil {
		_ = transaction.Commit()
	}
}

//nolint:lll // The grouped state query is the canonical source for terminal counters.
func itemStateCounts(ctx context.Context, transaction *sql.Tx, importID string) (map[string]int64, error) {
	counts := make(map[string]int64)
	rows, err := transaction.QueryContext(ctx, `SELECT state,count(*) FROM server_bios_import_items WHERE server_import_id=? GROUP BY state`, importID)
	if err != nil {
		return nil, fmt.Errorf("query server import item state counts: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan server import item state count: %w", err)
		}
		counts[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server import item state counts: %w", err)
	}
	return counts, nil
}

//nolint:lll // Terminal counters are projected from one immutable item-state snapshot.
func updateTerminalImport(ctx context.Context, transaction *sql.Tx, importID, state, phase string, code *string, counts map[string]int64, now int64) error {
	failed := counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] + counts["READ_FAILED"] + counts["COMMIT_FAILED"]
	var phaseValue any
	if phase != "" {
		phaseValue = phase
	}
	_, err := transaction.ExecContext(ctx, `UPDATE server_imports SET state=?,phase=?,last_error_code=?,
imported_matched_count=?,imported_warning_count=?,imported_missing_entry_count=?,not_found_count=?,
skipped_existing_count=?,skipped_not_better_count=?,same_bytes_count=?,failed_item_count=?,cancelled_item_count=?,
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?`,
		state, phaseValue, code, counts["IMPORTED_MATCHED"], counts["IMPORTED_WARNING"], counts["IMPORTED_MISSING_ENTRY"],
		counts["NOT_FOUND"], counts["SKIPPED_EXISTING"], counts["SKIPPED_NOT_BETTER"], counts["ALREADY_SAME_BYTES"],
		failed, counts["CANCELLED"], now, now, importID)
	if err != nil {
		return fmt.Errorf("update terminal server import: %w", err)
	}
	return nil
}
