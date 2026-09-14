package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

const summaryQuery = `
SELECT import.id,import.root_id,import.root_label_snapshot,import.source_relative_path,import.state,import.phase,
import.scan_job_id,import.import_job_id,import.gamelist_count,import.invalid_gamelist_count,import.collection_count,
import.folder_entry_count,import.game_count,import.estimated_source_bytes,
import.mapped_collection_count,import.skipped_collection_count,
import.skipped_mapping_item_count,
import.processable_item_count,import.blocked_item_count,import.review_pending_item_count,
import.published_item_count,import.review_discarded_item_count,import.existing_item_count,
import.failed_item_count,import.cancelled_item_count,import.media_warning_count,import.discovered_cover_count,
import.discovered_video_count,import.mapping_version,import.version,import.created_by_user_id,user.display_name,
import.last_error_code,
import.retryable,
import.created_at_ms,import.updated_at_ms,import.expires_at_ms,import.completed_at_ms
FROM emulationstation_imports import JOIN users user ON user.id=import.created_by_user_id`

type Queries struct{ database dbexec.Executor }

func NewQueries(database dbexec.Executor) *Queries { return &Queries{database: database} }

func scanSummary(row dbexec.Scanner) (application.Summary, error) {
	var result application.Summary
	var importJobID, phase, errorCode sql.NullString
	var retryable int
	if err := row.Scan(
		&result.ID, &result.Root.ID, &result.Root.Label, &result.SourceRelativePath, &result.State, &phase,
		&result.ScanJobID, &importJobID, &result.Counts.Gamelists, &result.Counts.InvalidGamelists,
		&result.Counts.Collections, &result.Counts.FoldersIgnored, &result.Counts.Games,
		&result.Counts.EstimatedSourceBytes, &result.Counts.MappedCollections,
		&result.Counts.SkippedCollections, &result.Counts.SkippedMapping, &result.Counts.Processable,
		&result.Counts.Blocked, &result.Counts.ReviewPending, &result.Counts.Published,
		&result.Counts.ReviewDiscarded, &result.Counts.Existing, &result.Counts.Failed,
		&result.Counts.Cancelled, &result.Counts.MediaWarnings, &result.Counts.Covers, &result.Counts.Videos,
		&result.MappingVersion, &result.Version, &result.CreatedBy.ID, &result.CreatedBy.DisplayName,
		&errorCode, &retryable, &result.CreatedAtMS, &result.UpdatedAtMS, &result.ExpiresAtMS, &result.CompletedAtMS,
	); err != nil {
		return application.Summary{}, fmt.Errorf("emulationstationimport/scan summary: %w", err)
	}
	result.Phase = nullableString(phase)
	result.ImportJobID = nullableString(importJobID)
	result.LastErrorCode = nullableString(errorCode)
	result.Retryable = retryable == 1
	return result, nil
}

func (service *Queries) Get(ctx context.Context, importID string) (application.Summary, error) {
	result, err := scanSummary(service.database.QueryRowContext(ctx, summaryQuery+` WHERE import.id=?`, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return application.Summary{}, application.ErrNotFound
	}
	return result, err
}

func (service *Queries) List(
	ctx context.Context,
	query application.ListQuery,
) ([]application.Summary, error) {
	state, beforeAt, beforeID, limit := query.State, query.BeforeAtMS, query.BeforeID, query.Limit
	if limit < 1 || limit > 21 {
		return nil, application.ErrInvalid
	}
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
		summaryQuery+` WHERE `+strings.Join(
			conditions,
			" AND ",
		)+` ORDER BY import.created_at_ms DESC,import.id DESC LIMIT ?`,
		arguments...)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/list: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]application.Summary, 0)
	for rows.Next() {
		value, scanErr := scanSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate summaries: %w", err)
	}
	return result, nil
}

func (service *Queries) requireImport(ctx context.Context, importID string) error {
	var present int
	if err := service.database.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM emulationstation_imports WHERE id=?)`,
		importID,
	).Scan(&present); err != nil {
		return fmt.Errorf("emulationstationimport/check import existence: %w", err)
	}
	if present == 0 {
		return application.ErrNotFound
	}
	return nil
}
