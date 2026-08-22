package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/firmware"
)

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

// Clearing candidates and their item projections is one resumable-discovery reset.
func (service *Service) clearEvaluation(ctx context.Context, importID string) error {
	if _, err := service.database.ExecContext(ctx, `
DELETE FROM server_bios_import_candidates WHERE server_import_id=?
`, importID); err != nil {
		return fmt.Errorf("clear server import candidates: %w", err)
	}
	_, err := service.database.ExecContext(
		ctx,
		`UPDATE server_bios_import_items SET state='PENDING',candidate_count=0,match_method=NULL,selection_details_json=NULL,
previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,completed_at_ms=NULL,updated_at_ms=?
WHERE server_import_id=? AND state IN ('PENDING','EVALUATING')`,
		service.now().UnixMilli(),
		importID,
	)
	if err != nil {
		return fmt.Errorf("reset server import items: %w", err)
	}
	return nil
}

// Candidate evidence, ranks and discovery completion commit atomically.
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
		count, multiple, err := persistCandidateGroup(ctx, transaction, unit, requirementID, values, now)
		if err != nil {
			return err
		}
		total += count
		if multiple {
			multi++
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET phase='RANKING',candidate_count=?,evaluated_item_count=catalog_item_count,
multi_candidate_item_count=?,skipped_special_count=?,skipped_unrepresentable_path_count=?,
version=version+1,updated_at_ms=? WHERE id=?
`, total, multi, counts.SkippedSpecial, counts.SkippedUnrepresentable, now, unit.ImportID); err != nil {
		return fmt.Errorf("complete server import discovery: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit server import candidates: %w", err)
	}
	return nil
}

type candidateDatabaseValues struct {
	md5, sha1, sha256, crc32                  any
	staticExact, staticSize, safe, launchable any
	matched, aliased, mismatched, missing     any
	extra, rank, notSelected                  any
}

func persistCandidateGroup(
	ctx context.Context,
	transaction *sql.Tx,
	unit work,
	requirementID string,
	candidates []*evaluatedCandidate,
	now int64,
) (int, bool, error) {
	ranked := rankCandidates(candidates)
	ranks := make(map[string]int, len(ranked))
	for index, candidate := range ranked {
		ranks[candidate.ID] = index + 1
	}
	for _, candidate := range candidates {
		if err := persistCandidateEvidence(
			ctx, transaction, unit, requirementID, candidate, ranks[candidate.ID], now,
		); err != nil {
			return 0, false, err
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='EVALUATING',candidate_count=?,updated_at_ms=?
WHERE server_import_id=? AND requirement_id=?
`, len(candidates), now, unit.ImportID, requirementID); err != nil {
		return 0, false, fmt.Errorf("update server import candidate count: %w", err)
	}
	return len(candidates), len(candidates) > 1, nil
}

func persistCandidateEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	unit work,
	requirementID string,
	candidate *evaluatedCandidate,
	rank int,
	now int64,
) error {
	details, _ := json.Marshal(candidate.Details)
	values := candidatePersistenceValues(candidate, rank)
	_, err := transaction.ExecContext(ctx, `
INSERT INTO server_bios_import_candidates(id,server_import_id,requirement_id,relative_path,basename,
association_kind,size_bytes,md5,sha1,sha256,crc32,state,exact_hash,expected_size_match,exact_basename,
safe_archive,launchable,matched_count,aliased_count,mismatched_count,missing_count,extra_count,rank_ordinal,
not_selected_reason,evaluation_details_json,created_at_ms,updated_at_ms,evaluated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, candidate.ID, unit.ImportID, requirementID, candidate.File.RelativePath, candidate.File.Basename,
		candidate.Association, candidate.File.SizeBytes, values.md5, values.sha1, values.sha256, values.crc32,
		candidate.State, values.staticExact, values.staticSize,
		boolInteger(candidate.File.Basename == candidate.Item.LogicalName), values.safe, values.launchable,
		values.matched, values.aliased, values.mismatched, values.missing, values.extra, values.rank,
		values.notSelected, string(details), now, now, now)
	if err != nil {
		return fmt.Errorf("persist server import candidate: %w", err)
	}
	return nil
}

func candidatePersistenceValues(candidate *evaluatedCandidate, rank int) candidateDatabaseValues {
	var values candidateDatabaseValues
	if candidate.Metadata.SHA256 != "" {
		values.md5, values.sha1 = candidate.Metadata.MD5, candidate.Metadata.SHA1
		values.sha256, values.crc32 = candidate.Metadata.SHA256, candidate.Metadata.CRC32
	}
	if candidate.Static != nil {
		values.staticExact = boolInteger(candidate.Static.ExactHash)
		values.staticSize = boolInteger(candidate.Static.ExpectedSizeMatched)
	}
	if candidate.DAT != nil {
		values.safe, values.launchable = 1, boolInteger(candidate.DAT.Launchable)
		values.matched, values.aliased = candidate.DAT.MatchedCount, candidate.DAT.AliasedCount
		values.mismatched, values.missing = candidate.DAT.MismatchedCount, candidate.DAT.MissingCount
		values.extra = candidate.DAT.ExtraCount
	}
	if rank > 0 {
		values.rank = rank
	}
	values.notSelected = candidateNotSelectedReason(candidate, rank)
	return values
}

func candidateNotSelectedReason(candidate *evaluatedCandidate, rank int) any {
	switch {
	case candidate.State == "DUPLICATE_BYTES":
		return "DUPLICATE_BYTES"
	case rank > 1:
		return "LOWER_RANK"
	case candidate.State != "ELIGIBLE":
		if code, ok := candidate.Details["code"].(string); ok {
			return code
		}
		return "INELIGIBLE"
	default:
		return nil
	}
}

// Progress updates deliberately mirror import, lease and event state in one helper.
func (service *Service) progress(ctx context.Context, unit work, phase string, current, total int64) {
	now := service.now().UnixMilli()
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "phase": phase, "completed": current, "total": total})
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE server_imports SET phase=?,version=version+1,updated_at_ms=? WHERE id=?`,
		phase,
		now,
		unit.ImportID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=? WHERE id=?`,
		now,
		now+60000,
		now,
		unit.JobID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	)
}

// Item outcome, selected candidate and progress event are committed together.
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state=?,match_method=?,selection_details_json=?,outcome_code=?,
previous_installation_id=?,new_installation_id=?,completed_at_ms=?,updated_at_ms=?
WHERE server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')
`, state, method, details, code, nil, nil, now, now, unit.ImportID, requirementID); err != nil {
		return
	}
	if candidate != nil {
		candidateState := candidate.State
		if state == "SOURCE_CHANGED" {
			candidateState = "SOURCE_CHANGED"
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_candidates SET state=?,not_selected_reason=?,updated_at_ms=? WHERE id=?
`, candidateState, code, now, candidate.ID); err != nil {
			return
		}
	}
	eventJSON, _ := json.Marshal(map[string]any{"schemaVersion": 1, "phase": "INSTALLING", "result": state})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)
`, unit.JobID, unit.ImportID, string(eventJSON), now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) cancelRequested(ctx context.Context, jobID string) bool {
	var state string
	return service.database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state) == nil &&
		(state == "CANCEL_REQUESTED" || state == "CANCELLED")
}

// Polling renews the lease before reading cancellation state.
func (service *Service) pollCancellation(ctx context.Context, unit work) bool {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`,
		now,
		now+60000,
		now,
		unit.JobID,
	)
	return service.cancelRequested(ctx, unit.JobID)
}

// Terminal import/job/event state is committed as one transaction.
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
	if err := updateTerminalImport(
		ctx, transaction, unit.ImportID, state, "QUEUEING_REVALIDATION", nil, counts, now,
	); err != nil {
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

// Failure item/import/job/event state is committed as one transaction.
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='COMMIT_FAILED',outcome_code=?,completed_at_ms=?,updated_at_ms=?
WHERE server_import_id=? AND state IN ('PENDING','EVALUATING')
`, code, now, now, unit.ImportID); err != nil {
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil || updateTerminalImport(ctx, transaction, unit.ImportID, "FAILED", "", &code, counts, now) != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=?,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, code, retryable, now, now, unit.JobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'FAILED',json_object('schemaVersion',1,'code',?),?)
`, unit.JobID, unit.ImportID, code, now); err == nil {
		_ = transaction.Commit()
	}
}

// Retry reset, lease release and retry event must remain one atomic state transition.
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
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM server_bios_import_candidates WHERE server_import_id=?
`, unit.ImportID); err != nil {
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

// Cancellation item/import/job/event state is committed as one transaction.
func (service *Service) cancelTask(ctx context.Context, unit work) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='CANCELLED',outcome_code='CANCELLED',
completed_at_ms=?,updated_at_ms=? WHERE server_import_id=? AND state IN ('PENDING','EVALUATING')
`, now, now, unit.ImportID); err != nil {
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil || updateTerminalImport(ctx, transaction, unit.ImportID, "CANCELLED", "", nil, counts, now) != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, now, now, now, unit.JobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'CANCELLED','{"schemaVersion":1}',?)
`, unit.JobID, unit.ImportID, now); err == nil {
		_ = transaction.Commit()
	}
}

func itemStateCounts(ctx context.Context, transaction *sql.Tx, importID string) (map[string]int64, error) {
	counts := make(map[string]int64)
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT state,count(*) FROM server_bios_import_items WHERE server_import_id=? GROUP BY state`,
		importID,
	)
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

func updateTerminalImport(
	ctx context.Context,
	transaction *sql.Tx,
	importID, state, phase string,
	code *string,
	counts map[string]int64,
	now int64,
) error {
	failed := counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] + counts["READ_FAILED"] + counts["COMMIT_FAILED"]
	var phaseValue any
	if phase != "" {
		phaseValue = phase
	}
	_, err := transaction.ExecContext(
		ctx,
		`UPDATE server_imports SET state=?,phase=?,last_error_code=?,
imported_matched_count=?,imported_warning_count=?,imported_missing_entry_count=?,not_found_count=?,
skipped_existing_count=?,skipped_not_better_count=?,same_bytes_count=?,failed_item_count=?,cancelled_item_count=?,
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?`,
		state,
		phaseValue,
		code,
		counts["IMPORTED_MATCHED"],
		counts["IMPORTED_WARNING"],
		counts["IMPORTED_MISSING_ENTRY"],
		counts["NOT_FOUND"],
		counts["SKIPPED_EXISTING"],
		counts["SKIPPED_NOT_BETTER"],
		counts["ALREADY_SAME_BYTES"],
		failed,
		counts["CANCELLED"],
		now,
		now,
		importID,
	)
	if err != nil {
		return fmt.Errorf("update terminal server import: %w", err)
	}
	return nil
}
