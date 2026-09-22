package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/contentquery"
	application "retrom/internal/service/libraryimport"
)

type ReviewBulkQueries struct{ executor dbexec.Executor }

func NewReviewBulkQueries(database *sql.DB) *ReviewBulkQueries {
	return &ReviewBulkQueries{executor: database}
}

func BindReviewBulkQueries(executor dbexec.Executor) *ReviewBulkQueries {
	return &ReviewBulkQueries{executor: executor}
}

// LatestReviewItemID returns the immutable upper bound used by bounded review
// scans. Keeping this projection next to the candidate query prevents callers
// from reaching into the database for cursor fencing.
func (repository *ReviewBulkQueries) LatestReviewItemID(ctx context.Context) (*string, error) {
	var value sql.NullString
	if err := repository.executor.QueryRowContext(ctx,
		`SELECT max(id) FROM import_items WHERE state='REVIEW_PENDING'`,
	).Scan(&value); err != nil {
		return nil, fmt.Errorf("query review item upper bound: %w", err)
	}
	if !value.Valid {
		//nolint:nilnil // a nil upper bound explicitly represents an empty review queue
		return nil, nil
	}
	return &value.String, nil
}

func (repository *ReviewBulkQueries) Candidates(
	ctx context.Context, query application.ReviewBulkCandidateQuery,
) ([]application.ReviewBulkCandidate, error) {
	statement, arguments, err := reviewBulkCandidateStatement(query)
	if err != nil {
		return nil, err
	}
	rows, err := repository.executor.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query review bulk candidates: %w", err)
	}
	defer func() { cleanup.Error("close review bulk candidates", rows.Close()) }()
	result := make([]application.ReviewBulkCandidate, 0, query.Limit)
	for rows.Next() {
		candidate, err := scanReviewBulkCandidate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review bulk candidates: %w", err)
	}
	return result, nil
}

func reviewBulkCandidateStatement(query application.ReviewBulkCandidateQuery) (string, []any, error) {
	if query.Limit < 1 || query.Limit > application.ReviewBulkQueryLimit {
		return "", nil, application.ErrReviewBulkQuery
	}
	statement := reviewBulkCandidateSelect
	arguments := make([]any, 0, 12)
	for _, filter := range []struct {
		value, condition string
	}{
		{query.Scope.ImportJobID, " AND item.import_job_id=?"},
		{query.Scope.SourceImportID, " AND source_owner.import_id=?"},
		{query.Scope.PlatformInstanceID, " AND draft.target_platform_instance_id=?"},
	} {
		if filter.value != "" {
			statement += filter.condition
			arguments = append(arguments, filter.value)
		}
	}
	if query.Scope.Q != "" {
		statement += ` AND (instr(item.search_text,?)>0 OR EXISTS(
 SELECT 1 FROM review_draft_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=draft.id AND instr(tag.name_key,?)>0))`
		arguments = append(arguments, query.Scope.Q, query.Scope.Q)
	}
	if query.Scope.TagID != "" {
		statement += ` AND EXISTS(SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=draft.id AND tag.id=?)`
		arguments = append(arguments, query.Scope.TagID)
	}
	if query.Scope.BlockerCode != "" {
		statement += " AND (validation.compatibility_code=? OR (?='NEEDS_VALIDATION' AND validation.id IS NULL))"
		arguments = append(arguments, query.Scope.BlockerCode, query.Scope.BlockerCode)
	}
	if query.AfterItemID != "" {
		statement += " AND item.id>?"
		arguments = append(arguments, query.AfterItemID)
	}
	if query.ThroughItemID != "" {
		statement += " AND item.id<=?"
		arguments = append(arguments, query.ThroughItemID)
	}
	statement += " ORDER BY item.id LIMIT ?"
	arguments = append(arguments, query.Limit)
	return statement, arguments, nil
}

const reviewBulkCandidateSelect = `
SELECT item.id,draft.version,draft.effective_source_snapshot_id,
       json_extract(draft.metadata_json,'$.title'),instance.id,instance.name,instance.platform_id,instance.version,
       validation.provider_id,validation.target_id,
       CASE WHEN binding.binding_id IS NULL THEN NULL ELSE ` + contentquery.BindingPolicySQL + ` END,
       validation.id,validation.status,
       validation.platform_instance_version,
       validation.dat_version_id,
       (SELECT active.id FROM dat_versions active
         WHERE active.provider_id=validation.provider_id
         AND active.target_id=validation.target_id AND active.is_active=1),
       validation.default_dos_entry,draft.default_dos_entry,validation.dependency_snapshot_json,
       source.content_kind,
       EXISTS(SELECT 1 FROM review_runtime_screenshots screenshot
         WHERE screenshot.import_item_id=item.id AND screenshot.validation_id=validation.id
         AND screenshot.source_snapshot_id=draft.effective_source_snapshot_id
         AND screenshot.provider_id=validation.provider_id AND screenshot.target_id=validation.target_id),
       EXISTS(SELECT 1 FROM review_arcade_parent_attachments attachment
         WHERE attachment.import_item_id=item.id AND attachment.state IN ('QUEUED','RUNNING')) OR
       EXISTS(SELECT 1 FROM review_multidisc_attachments attachment
         WHERE attachment.import_item_id=item.id AND attachment.state IN ('QUEUED','RUNNING')),
       COALESCE(json_extract(source_owner.source_flags_json,'$.hidden'),0)=1 OR
       COALESCE(json_extract(source_owner.source_flags_json,'$.adult'),0)=1
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots source ON source.id=draft.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
LEFT JOIN rpgmaker_review_profiles rpg_profile ON rpg_profile.review_draft_id=draft.id
LEFT JOIN import_item_core_validations validation ON validation.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=item.id
  AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
  AND candidate.target_platform_instance_id=draft.target_platform_instance_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
LEFT JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
LEFT JOIN runtime_target_bindings binding
  ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.core_id=validation.core_id AND binding.launch_policy!='DISABLED'
LEFT JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id
LEFT JOIN source_import_items source_owner ON source_owner.library_import_item_id=item.id
WHERE item.state='REVIEW_PENDING'
AND (source_owner.id IS NULL OR source_owner.execution_state='REVIEW_PENDING')`

func scanReviewBulkCandidate(scanner dbexec.Scanner) (application.ReviewBulkCandidate, error) {
	var candidate application.ReviewBulkCandidate
	var title, providerID, targetID, validationID, validationStatus sql.NullString
	var validationPlatformVersion sql.NullInt64
	var validationDAT, currentDAT, validationDOSEntry, draftDOSEntry, dependencySnapshot sql.NullString
	if err := scanner.Scan(
		&candidate.ItemID, &candidate.ReviewVersion, &candidate.SourceSnapshotID,
		&title, &candidate.PlatformInstanceID, &candidate.PlatformName, &candidate.PlatformID,
		&candidate.PlatformVersion, &providerID, &targetID,
		contentquery.ScanPolicy(&candidate.ContentPolicy), &validationID, &validationStatus,
		&validationPlatformVersion, &validationDAT, &currentDAT, &validationDOSEntry,
		&draftDOSEntry, &dependencySnapshot, &candidate.ContentKind,
		&candidate.ScreenshotCurrent, &candidate.AttachmentActive, &candidate.SourceFlagged,
	); err != nil {
		return application.ReviewBulkCandidate{}, fmt.Errorf("scan review bulk candidate: %w", err)
	}
	candidate.Title = title.String
	candidate.ProviderID = nullableReviewBulkString(providerID)
	candidate.TargetID = nullableReviewBulkString(targetID)
	candidate.ValidationID = nullableReviewBulkString(validationID)
	candidate.ValidationStatus = nullableReviewBulkString(validationStatus)
	candidate.ValidationPlatformVersion = nullableReviewBulkInt(validationPlatformVersion)
	candidate.ValidationDAT = nullableReviewBulkString(validationDAT)
	candidate.CurrentDAT = nullableReviewBulkString(currentDAT)
	candidate.ValidationDOSEntry = nullableReviewBulkString(validationDOSEntry)
	candidate.DraftDOSEntry = nullableReviewBulkString(draftDOSEntry)
	candidate.DependencySnapshot = nullableReviewBulkString(dependencySnapshot)
	return candidate, nil
}

func nullableReviewBulkString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableReviewBulkInt(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func (repository *ReviewBulkQueries) Items(
	ctx context.Context, query application.ReviewBulkItemQuery,
) ([]application.ReviewBulkItemRecord, error) {
	statement, arguments, err := reviewBulkItemStatement(query)
	if err != nil {
		return nil, err
	}
	rows, err := repository.executor.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query review bulk items: %w", err)
	}
	defer func() { cleanup.Error("close review bulk items", rows.Close()) }()
	result := make([]application.ReviewBulkItemRecord, 0, query.Limit)
	for rows.Next() {
		var item application.ReviewBulkItemRecord
		var gameID, code, details sql.NullString
		var completed sql.NullInt64
		if err := rows.Scan(&item.ImportItemID, &item.Title, &item.PlatformName, &item.State,
			&gameID, &code, &details, &completed, &item.Ordinal); err != nil {
			return nil, fmt.Errorf("scan review bulk item: %w", err)
		}
		item.GameID = nullableReviewBulkString(gameID)
		item.OutcomeCode = nullableReviewBulkString(code)
		item.CompletedAtMS = nullableReviewBulkInt(completed)
		if details.Valid {
			item.OutcomeDetails = json.RawMessage(details.String)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review bulk items: %w", err)
	}
	return result, nil
}

func reviewBulkItemStatement(query application.ReviewBulkItemQuery) (string, []any, error) {
	if query.BulkApprovalID == "" || query.AfterOrdinal < -1 || query.Limit < 1 ||
		query.Limit > application.ReviewBulkItemLimit+1 {
		return "", nil, application.ErrReviewBulkQuery
	}
	statement := `SELECT import_item_id,title_snapshot,target_platform_name_snapshot,state,game_id,
outcome_code,outcome_details_json,completed_at_ms,ordinal
FROM review_bulk_approval_items WHERE bulk_approval_id=? AND ordinal>?`
	arguments := []any{query.BulkApprovalID, query.AfterOrdinal}
	if query.Outcome != "" {
		statement += " AND state=?"
		arguments = append(arguments, query.Outcome)
	}
	statement += " ORDER BY ordinal LIMIT ?"
	return statement, append(arguments, query.Limit), nil
}

const reviewBulkSummarySelect = `
SELECT bulk.id,bulk.job_id,bulk.state,bulk.version,bulk.scope_json,
       bulk.matched_count,bulk.candidate_count,bulk.screenshot_only_count,
       bulk.duplicate_count,bulk.attachment_active_count,bulk.source_flagged_count,
       bulk.not_ready_or_stale_count,
       bulk.candidate_count,bulk.processed_count,bulk.published_count,
       bulk.skipped_duplicate_count,bulk.skipped_changed_count,bulk.skipped_not_ready_count,
       bulk.failed_count,bulk.cancelled_count,bulk.created_at_ms,bulk.started_at_ms,
       bulk.updated_at_ms,bulk.completed_at_ms,bulk.last_error_code
FROM review_bulk_approvals bulk`

func (repository *ReviewBulkQueries) Summary(
	ctx context.Context, bulkID string,
) (application.ReviewBulkSummary, error) {
	return scanReviewBulkSummary(repository.executor.QueryRowContext(
		ctx, reviewBulkSummarySelect+" WHERE bulk.id=?", bulkID,
	))
}

func (repository *ReviewBulkQueries) ActiveSummary(
	ctx context.Context,
) (application.ReviewBulkSummary, bool, error) {
	result, err := scanReviewBulkSummary(repository.executor.QueryRowContext(
		ctx, reviewBulkSummarySelect+" WHERE bulk.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') LIMIT 1",
	))
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewBulkSummary{}, false, nil
	}
	if err != nil {
		return application.ReviewBulkSummary{}, false, err
	}
	return result, true, nil
}

func scanReviewBulkSummary(scanner dbexec.Scanner) (application.ReviewBulkSummary, error) {
	var result application.ReviewBulkSummary
	var scopeJSON string
	var startedAt, completedAt sql.NullInt64
	var lastError sql.NullString
	if err := scanner.Scan(
		&result.BulkApprovalID, &result.JobID, &result.State, &result.Version, &scopeJSON,
		&result.Counts.Matched, &result.Counts.StrictReady, &result.Counts.ScreenshotOnly,
		&result.Counts.Duplicate, &result.Counts.AttachmentActive, &result.Counts.SourceFlagged,
		&result.Counts.NotReadyOrStale,
		&result.Progress.Candidate, &result.Progress.Processed, &result.Progress.Published,
		&result.Progress.SkippedDuplicate, &result.Progress.SkippedChanged,
		&result.Progress.SkippedNotReady, &result.Progress.Failed, &result.Progress.Cancelled,
		&result.CreatedAtMS, &startedAt, &result.UpdatedAtMS, &completedAt, &lastError,
	); err != nil {
		return application.ReviewBulkSummary{}, fmt.Errorf("scan review bulk summary: %w", err)
	}
	if err := json.Unmarshal([]byte(scopeJSON), &result.Scope); err != nil {
		return application.ReviewBulkSummary{}, fmt.Errorf("decode review bulk scope: %w", err)
	}
	result.StartedAtMS = nullableReviewBulkInt(startedAt)
	result.CompletedAtMS = nullableReviewBulkInt(completedAt)
	result.LastErrorCode = nullableReviewBulkString(lastError)
	return result, nil
}

var _ application.ReviewBulkRepository = (*ReviewBulkQueries)(nil)
