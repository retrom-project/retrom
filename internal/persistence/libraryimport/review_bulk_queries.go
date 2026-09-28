package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewBulkQueries struct{ executor dbapi.Executor }

func NewReviewBulkQueries(database dbapi.DB) *ReviewBulkQueries {
	return &ReviewBulkQueries{executor: database}
}

func BindReviewBulkQueries(executor dbapi.Executor) *ReviewBulkQueries {
	return &ReviewBulkQueries{executor: executor}
}

// LatestReviewItemID returns the immutable upper bound used by bounded review
// scans. Keeping this projection next to the candidate query prevents callers
// from reaching into the database for cursor fencing.
func (repository *ReviewBulkQueries) LatestReviewItemID(ctx context.Context) (*string, error) {
	var value sql.NullString
	if err := dbapi.QueryRowContext(ctx, repository.executor,
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
	ctx context.Context, query libraryservice.ReviewBulkCandidateQuery,
) ([]libraryservice.ReviewBulkCandidate, error) {
	statement, arguments, err := reviewBulkCandidateStatement(query)
	if err != nil {
		return nil, err
	}
	rows, err := repository.executor.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query review bulk candidates: %w", err)
	}
	defer func() { cleanup.Error("close review bulk candidates", rows.Close()) }()
	result := make([]libraryservice.ReviewBulkCandidate, 0, query.Limit)
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

func reviewBulkCandidateStatement(query libraryservice.ReviewBulkCandidateQuery) (string, []any, error) {
	if query.Limit < 1 || query.Limit > libraryservice.ReviewBulkQueryLimit {
		return "", nil, libraryservice.ErrReviewBulkQuery
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
SELECT item.id,draft.review_version,draft.effective_source_snapshot_id,instance.platform_id,
       validation.id,validation.status,
       EXISTS(SELECT 1 FROM review_arcade_parent_attachments attachment
         WHERE attachment.import_item_id=item.id AND attachment.state IN ('QUEUED','RUNNING')) OR
       EXISTS(SELECT 1 FROM review_multidisc_attachments attachment
         WHERE attachment.import_item_id=item.id AND attachment.state IN ('QUEUED','RUNNING')),
       COALESCE(json_extract(source_owner.source_flags_json,'$.hidden'),0)=1 OR
       COALESCE(json_extract(source_owner.source_flags_json,'$.adult'),0)=1
FROM import_items item
JOIN import_items draft ON draft.id=item.id
JOIN import_item_source_snapshots source ON source.id=draft.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
LEFT JOIN import_item_core_validations validation ON validation.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=item.id
  AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
  AND candidate.target_platform_instance_id=draft.target_platform_instance_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
LEFT JOIN source_import_items source_owner ON source_owner.library_import_item_id=item.id
WHERE item.state='REVIEW_PENDING'
AND (source_owner.id IS NULL OR source_owner.execution_state='REVIEW_PENDING')`

func scanReviewBulkCandidate(scanner dbapi.Scanner) (libraryservice.ReviewBulkCandidate, error) {
	var candidate libraryservice.ReviewBulkCandidate
	if err := scanner.Scan(&candidate.ItemID, &candidate.ReviewVersion, &candidate.SourceSnapshotID,
		&candidate.PlatformID, &candidate.ValidationID, &candidate.ValidationStatus,
		&candidate.AttachmentActive, &candidate.SourceFlagged,
	); err != nil {
		return libraryservice.ReviewBulkCandidate{}, fmt.Errorf("scan review bulk candidate: %w", err)
	}
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

func (repository *ReviewBulkQueries) CandidateByID(
	ctx context.Context, itemID string,
) (libraryservice.ReviewBulkCandidate, bool, error) {
	result, err := scanReviewBulkCandidate(dbapi.QueryRowContext(
		ctx, repository.executor,
		reviewBulkCandidateSelect+" AND item.id=?", itemID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewBulkCandidate{}, false, nil
	}
	return result, err == nil, err
}

const reviewBulkSummarySelect = `
SELECT id,job_id,state,version,max_item_id,cursor_item_id,initial_pending_count,
scanned_count,published_count,skipped_changed_count,skipped_duplicate_count,skipped_not_ready_count,
created_at_ms,updated_at_ms,started_at_ms,completed_at_ms,last_error_code
FROM review_bulk_approvals`

func (repository *ReviewBulkQueries) Summary(
	ctx context.Context, bulkID string,
) (libraryservice.ReviewBulkSummary, error) {
	return scanReviewBulkSummary(dbapi.QueryRowContext(
		ctx, repository.executor,
		reviewBulkSummarySelect+" WHERE id=?", bulkID,
	))
}

func (repository *ReviewBulkQueries) ActiveSummary(
	ctx context.Context,
) (libraryservice.ReviewBulkSummary, bool, error) {
	result, err := scanReviewBulkSummary(dbapi.QueryRowContext(
		ctx, repository.executor,
		reviewBulkSummarySelect+" WHERE state IN ('QUEUED','RUNNING') LIMIT 1",
	))
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewBulkSummary{}, false, nil
	}
	return result, err == nil, err
}

func scanReviewBulkSummary(scanner dbapi.Scanner) (libraryservice.ReviewBulkSummary, error) {
	var result libraryservice.ReviewBulkSummary
	var cursor, lastError sql.NullString
	var started, completed sql.NullInt64
	if err := scanner.Scan(
		&result.BulkApprovalID, &result.JobID, &result.State, &result.Version,
		&result.MaxItemID, &cursor, &result.InitialPendingCount, &result.ScannedCount,
		&result.PublishedCount, &result.SkippedChangedCount, &result.SkippedDuplicateCount,
		&result.SkippedNotReadyCount, &result.CreatedAtMS, &result.UpdatedAtMS,
		&started, &completed, &lastError,
	); err != nil {
		return libraryservice.ReviewBulkSummary{}, fmt.Errorf("scan review bulk summary: %w", err)
	}
	result.CursorItemID = nullableReviewBulkString(cursor)
	result.StartedAtMS = nullableReviewBulkInt(started)
	result.CompletedAtMS = nullableReviewBulkInt(completed)
	result.LastErrorCode = nullableReviewBulkString(lastError)
	return result, nil
}
