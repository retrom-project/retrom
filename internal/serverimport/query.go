package serverimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

func (service *Service) Get(ctx context.Context, importID string) (Summary, error) {
	result, err := scanSummary(service.database.QueryRowContext(ctx, summaryQuery+` WHERE import.id=?`, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	return result, err
}

const summaryQuery = `
SELECT import.id,import.kind,import.root_id,import.root_label_snapshot,import.source_relative_path,
import.replace_if_better,import.state,import.phase,import.catalog_item_count,import.candidate_count,
import.evaluated_item_count,import.imported_matched_count,import.imported_warning_count,
import.imported_missing_entry_count,import.not_found_count,import.skipped_existing_count,
import.skipped_not_better_count,import.same_bytes_count,import.failed_item_count,import.cancelled_item_count,
import.job_id,import.created_by_user_id,user.display_name,import.last_error_code,import.version,
import.created_at_ms,import.updated_at_ms,import.completed_at_ms
FROM server_imports import JOIN users user ON user.id=import.created_by_user_id`

type rowScanner interface{ Scan(...any) error }

// The fixed aggregate projection is scanned in schema order for auditability.
func scanSummary(row rowScanner) (Summary, error) {
	var summary Summary
	var replace int
	var catalog, candidates, evaluated, matched, warnings, missingEntry int64
	var notFound, skippedExisting, skippedNotBetter, same, failed, cancelled int64
	if err := row.Scan(
		&summary.ID, &summary.Kind, &summary.Root.ID, &summary.Root.Label, &summary.SourceRelativePath,
		&replace, &summary.State, &summary.Phase, &catalog, &candidates, &evaluated, &matched, &warnings, &missingEntry,
		&notFound, &skippedExisting, &skippedNotBetter, &same, &failed, &cancelled, &summary.JobID,
		&summary.CreatedBy.ID, &summary.CreatedBy.DisplayName, &summary.LastErrorCode, &summary.Version,
		&summary.CreatedAtMS, &summary.UpdatedAtMS, &summary.CompletedAtMS,
	); err != nil {
		return Summary{}, fmt.Errorf("serverimport/scan summary: %w", err)
	}
	summary.ReplaceIfBetter = replace == 1
	summary.Counts = Counts{
		CatalogItems: catalog, Candidates: candidates, EvaluatedItems: evaluated,
		Imported: matched + warnings + missingEntry, Matched: matched, Warnings: warnings + missingEntry,
		NotFound: notFound, Skipped: skippedExisting + skippedNotBetter + same,
		Conflicts: skippedNotBetter, Failed: failed, Cancelled: cancelled,
	}
	return summary, nil
}

// The keyset predicate and stable ordering stay adjacent to the query call.
func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]Summary, error) {
	conditions := []string{"1=1"}
	arguments := []any{}
	if state != "" {
		conditions = append(conditions, "import.state=?")
		arguments = append(arguments, state)
	}
	if beforeID != "" {
		conditions = append(conditions, "(import.created_at_ms<? OR (import.created_at_ms=? AND import.id<?))")
		arguments = append(arguments, beforeAt, beforeAt, beforeID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(
		ctx,
		summaryQuery+" WHERE "+strings.Join(
			conditions,
			" AND ",
		)+" ORDER BY import.created_at_ms DESC,import.id DESC LIMIT ?",
		arguments...)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list summaries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Summary, 0)
	for rows.Next() {
		summary, scanErr := scanSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate summaries: %w", err)
	}
	return result, nil
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, method string,
	afterCore, afterName, afterID string,
	limit int,
) ([]Item, error) {
	conditions := []string{"item.server_import_id=?"}
	arguments := []any{importID}
	if query != "" {
		conditions = append(
			conditions,
			"(instr(lower(item.logical_name),lower(?))>0 OR "+
				"instr(lower(item.core_name_snapshot),lower(?))>0 OR instr(lower(item.core_id),lower(?))>0)",
		)
		arguments = append(arguments, query, query, query)
	}
	if outcome != "" {
		conditions = append(conditions, "item.state=?")
		arguments = append(arguments, outcome)
	}
	if method != "" {
		conditions = append(conditions, "item.match_method=?")
		arguments = append(arguments, method)
	}
	if afterID != "" {
		conditions = append(
			conditions,
			"(item.core_name_snapshot>? OR (item.core_name_snapshot=? AND item.logical_name>?) OR "+
				"(item.core_name_snapshot=? AND item.logical_name=? AND item.requirement_id>?))",
		)
		arguments = append(arguments, afterCore, afterCore, afterName, afterCore, afterName, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(ctx, `
SELECT item.requirement_id,item.core_id,item.core_name_snapshot,item.provider_id,item.target_id,
item.logical_name,
item.requirement_mode,
item.source_kind,item.state,item.candidate_count,item.match_method,item.outcome_code,item.selection_details_json,
selected.relative_path,previous.status,replacement.status,
CASE WHEN item.previous_installation_id IS NOT NULL AND item.new_installation_id IS NOT NULL
          AND item.previous_installation_id<>item.new_installation_id THEN 1 ELSE 0 END
FROM server_bios_import_items item
LEFT JOIN server_bios_import_candidates selected ON selected.server_import_id=item.server_import_id
 AND selected.requirement_id=item.requirement_id AND selected.state='SELECTED'
LEFT JOIN bios_installations previous ON previous.id=item.previous_installation_id
LEFT JOIN bios_installations replacement ON replacement.id=item.new_installation_id
WHERE `+strings.Join(conditions, " AND ")+`
 ORDER BY item.core_name_snapshot COLLATE BINARY,item.logical_name COLLATE BINARY,
 item.requirement_id COLLATE BINARY LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Item, 0)
	for rows.Next() {
		var item Item
		var details sql.NullString
		var replaced int
		if err := rows.Scan(
			&item.RequirementID, &item.CoreID, &item.CoreName, &item.ProviderID, &item.TargetID,
			&item.LogicalName,
			&item.RequirementMode, &item.SourceKind, &item.State, &item.CandidateCount, &item.MatchMethod,
			&item.OutcomeCode, &details, &item.SelectedRelativePath, &item.PreviousInstallationStatus,
			&item.NewInstallationStatus, &replaced,
		); err != nil {
			return nil, fmt.Errorf("serverimport/scan item: %w", err)
		}
		item.Replaced = replaced == 1
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &item.SelectionDetails)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate items: %w", err)
	}
	return result, nil
}

// Candidate evidence is decoded in the same order as its stable keyset query.
func (service *Service) Candidates(
	ctx context.Context,
	importID, requirementID string,
	afterRank int64,
	afterID string,
	limit int,
) ([]Candidate, error) {
	var exists int
	if err := service.database.QueryRowContext(ctx, `
SELECT 1 FROM server_bios_import_items WHERE server_import_id=? AND requirement_id=?
`, importID, requirementID).Scan(&exists); errors.Is(
		err,
		sql.ErrNoRows,
	) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("serverimport/check candidate item: %w", err)
	}
	arguments := []any{importID, requirementID}
	watermark := ""
	if afterID != "" {
		watermark = " AND (COALESCE(rank_ordinal,9223372036854775807)>? OR " +
			"(COALESCE(rank_ordinal,9223372036854775807)=? AND id>?))"
		arguments = append(arguments, afterRank, afterRank, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(ctx, `
SELECT id,relative_path,basename,association_kind,size_bytes,md5,sha1,sha256,crc32,state,
rank_ordinal,not_selected_reason,evaluation_details_json
FROM server_bios_import_candidates WHERE server_import_id=? AND requirement_id=?`+watermark+`
 ORDER BY COALESCE(rank_ordinal,9223372036854775807),id LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Candidate, 0)
	for rows.Next() {
		var candidate Candidate
		var details sql.NullString
		if err := rows.Scan(
			&candidate.ID, &candidate.RelativePath, &candidate.Basename, &candidate.AssociationKind,
			&candidate.SizeBytes, &candidate.MD5, &candidate.SHA1, &candidate.SHA256, &candidate.CRC32,
			&candidate.State, &candidate.RankOrdinal, &candidate.NotSelectedReason, &details,
		); err != nil {
			return nil, fmt.Errorf("serverimport/scan candidate: %w", err)
		}
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &candidate.EvaluationDetails)
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate candidates: %w", err)
	}
	return result, nil
}

// Cancellation item, job, aggregate, event and audit writes share one transaction.
func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	version int64,
	reason, userID string,
) (Summary, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Summary{}, false, ErrNotCancellable
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/begin cancel transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, jobID string
	var actualVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,version,job_id FROM server_imports WHERE id=?
`, importID).Scan(&state, &actualVersion, &jobID); err != nil ||
		actualVersion != version ||
		state != "QUEUED" && state != "RUNNING" {
		return Summary{}, false, ErrNotCancellable
	}
	now := service.now().UnixMilli()
	pending := state == "RUNNING"
	newState := "CANCELLED"
	jobState := "CANCELLED"
	var completed any = now
	if pending {
		newState = "CANCEL_REQUESTED"
		jobState = "CANCEL_REQUESTED"
		completed = nil
	} else {
		if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='CANCELLED',outcome_code='CANCELLED',
completed_at_ms=?,updated_at_ms=? WHERE server_import_id=?
`, now, now, importID); err != nil {
			return Summary{}, false, fmt.Errorf("serverimport/cancel queued items: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=?
`, jobState, now, reason, completed, now, jobID); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/cancel job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state=?,cancel_requested_at_ms=?,cancel_reason=?,
cancelled_item_count=CASE WHEN ?='CANCELLED' THEN catalog_item_count ELSE cancelled_item_count END,
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?
`, newState, now, reason, newState, completed, now, importID); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/cancel aggregate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'CANCEL_REQUESTED',json_object('schemaVersion',1),?)
`, jobID, importID, now); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/write cancel event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'SERVER_IMPORT_CANCEL_REQUESTED','SERVER_IMPORT',?,'{}','{}',NULL,NULL,?)
`, auditID.String(), userID, importID, now); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/write cancel audit: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, false, fmt.Errorf("serverimport/commit cancel transaction: %w", err)
	}
	summary, err := service.Get(ctx, importID)
	return summary, pending, err
}

// Retry reset, new immutable input and audit/event writes form one transaction.
func (service *Service) Retry(ctx context.Context, importID string, version int64, userID string) (Summary, error) {
	plan, err := service.prepareRetry(ctx, importID, version)
	if err != nil {
		return Summary{}, err
	}
	if err := service.resetRetry(ctx, plan, version, userID); err != nil {
		return Summary{}, err
	}
	service.signal()
	return service.Get(ctx, importID)
}

type retryPlan struct {
	importID, storedDigest, catalogDigest, jobID string
	rootSummary                                  Summary
}

func (service *Service) prepareRetry(ctx context.Context, importID string, version int64) (retryPlan, error) {
	rootSummary, err := service.Get(ctx, importID)
	if err != nil || rootSummary.State != "FAILED" || rootSummary.Version != version ||
		rootSummary.LastErrorCode == nil ||
		(*rootSummary.LastErrorCode != "SERVER_IMPORT_ROOT_UNAVAILABLE" && *rootSummary.LastErrorCode != "INTERNAL_ERROR") {
		return retryPlan{}, ErrNotRetryable
	}
	root, ok := service.roots[rootSummary.Root.ID]
	if !ok {
		return retryPlan{}, ErrNotRetryable
	}
	var storedDigest, catalogDigest, jobID string
	if err := service.database.QueryRowContext(ctx, `
SELECT root_config_digest,catalog_snapshot_digest,job_id FROM server_imports WHERE id=?
`, importID).Scan(&storedDigest, &catalogDigest, &jobID); err != nil ||
		storedDigest != root.digest {
		return retryPlan{}, ErrNotRetryable
	}
	return retryPlan{importID, storedDigest, catalogDigest, jobID, rootSummary}, nil
}

func (service *Service) resetRetry(ctx context.Context, plan retryPlan, version int64, userID string) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("serverimport/begin retry transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var execution int64
	if err := transaction.QueryRowContext(ctx, `SELECT execution_no FROM jobs WHERE id=?`, plan.jobID).
		Scan(&execution); err != nil {
		return fmt.Errorf("serverimport/read retry execution number: %w", err)
	}
	execution++
	now := service.now().UnixMilli()
	inputID, _ := uuid.NewV7()
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_BIOS_IMPORT",
		"scope":         map[string]any{"type": "SERVER_IMPORT", "id": plan.importID},
		"executionId":   inputID.String(),
		"inputs": map[string]any{
			"serverImportVersion":   version,
			"rootId":                plan.rootSummary.Root.ID,
			"sourceRelativePath":    plan.rootSummary.SourceRelativePath,
			"rootConfigDigest":      plan.storedDigest,
			"catalogSnapshotDigest": plan.catalogDigest,
			"replaceIfBetter":       plan.rootSummary.ReplaceIfBetter,
		},
	}
	encoded, _ := json.Marshal(input)
	digest := fmt.Sprintf("%x", sha256Bytes(encoded))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)
`, plan.jobID, execution, string(encoded), digest, now); err != nil {
		return fmt.Errorf("serverimport/write retry input snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',execution_no=?,payload_json=json_object('inputExecutionNo',?),
attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL,finished_at_ms=NULL,worker_id=NULL,error_code=NULL,
error_retryable=NULL,cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, execution, execution, now, now, plan.jobID); err != nil {
		return fmt.Errorf("serverimport/reset retry job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM server_bios_import_candidates WHERE server_import_id=?
`, plan.importID); err != nil {
		return fmt.Errorf("serverimport/clear retry candidates: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='PENDING',candidate_count=0,match_method=NULL,
selection_details_json=NULL,previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,
completed_at_ms=NULL,updated_at_ms=? WHERE server_import_id=?
`, now, plan.importID); err != nil {
		return fmt.Errorf("serverimport/reset retry items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='QUEUED',phase=NULL,candidate_count=0,evaluated_item_count=0,
multi_candidate_item_count=0,imported_matched_count=0,imported_warning_count=0,
imported_missing_entry_count=0,not_found_count=0,skipped_existing_count=0,skipped_not_better_count=0,
same_bytes_count=0,failed_item_count=0,cancelled_item_count=0,last_error_code=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, now, plan.importID); err != nil {
		return fmt.Errorf("serverimport/reset retry aggregate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'MANUAL_RETRY',json_object('schemaVersion',1,'executionNo',?),?)
`, plan.jobID, plan.importID, execution, now); err != nil {
		return fmt.Errorf("serverimport/write retry event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'SERVER_IMPORT_RETRIED','SERVER_IMPORT',?,'{}','{}',NULL,NULL,?)
`, auditID.String(), userID, plan.importID, now); err != nil {
		return fmt.Errorf("serverimport/write retry audit: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("serverimport/commit retry transaction: %w", err)
	}
	return nil
}

func sha256Bytes(value []byte) [32]byte { return sha256.Sum256(value) }
