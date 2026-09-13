package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/pegasusimport"
)

const summaryQuery = `
SELECT import.id,import.root_id,import.root_label_snapshot,import.source_relative_path,import.state,import.phase,
import.scan_job_id,import.import_job_id,import.metadata_count,import.invalid_metadata_count,import.collection_count,
import.game_count,import.estimated_source_bytes,import.mapped_collection_count,import.skipped_collection_count,
import.processable_item_count,import.blocked_item_count,import.review_pending_item_count,
import.published_item_count,import.review_discarded_item_count,import.existing_item_count,
import.failed_item_count,import.cancelled_item_count,import.media_warning_count,import.discovered_cover_count,
import.discovered_video_count,import.mapping_version,import.version,import.created_by_user_id,user.display_name,
import.last_error_code,
import.retryable,
import.created_at_ms,import.updated_at_ms,import.expires_at_ms,import.completed_at_ms
FROM pegasus_imports import JOIN users user ON user.id=import.created_by_user_id`

type Queries struct{ database dbexec.Executor }

func NewQueries(database *sql.DB) *Queries { return &Queries{database: database} }

func scanSummary(row dbexec.Scanner) (application.Summary, error) {
	var result application.Summary
	var importJobID, phase, errorCode sql.NullString
	var retryable int
	if err := row.Scan(
		&result.ID, &result.Root.ID, &result.Root.Label, &result.SourceRelativePath, &result.State, &phase,
		&result.ScanJobID, &importJobID, &result.Counts.Metadata, &result.Counts.InvalidMetadata,
		&result.Counts.Collections, &result.Counts.Games, &result.Counts.EstimatedSourceBytes,
		&result.Counts.MappedCollections, &result.Counts.SkippedCollections, &result.Counts.Processable,
		&result.Counts.Blocked, &result.Counts.ReviewPending, &result.Counts.Published,
		&result.Counts.ReviewDiscarded, &result.Counts.Existing, &result.Counts.Failed,
		&result.Counts.Cancelled, &result.Counts.MediaWarnings, &result.Counts.Covers, &result.Counts.Videos,
		&result.MappingVersion, &result.Version, &result.CreatedBy.ID, &result.CreatedBy.DisplayName,
		&errorCode, &retryable, &result.CreatedAtMS, &result.UpdatedAtMS, &result.ExpiresAtMS, &result.CompletedAtMS,
	); err != nil {
		return application.Summary{}, fmt.Errorf("pegasusimport/scan summary: %w", err)
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
		return nil, fmt.Errorf("pegasusimport/list: %w", err)
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
		return nil, fmt.Errorf("pegasusimport/iterate summaries: %w", err)
	}
	return result, nil
}

func (service *Queries) Collections(
	ctx context.Context,
	query application.CollectionQuery,
) ([]application.CollectionRecord, error) {
	importID, afterPath, afterOrdinal := query.ImportID, query.AfterPath, query.AfterOrdinal
	afterID, limit := query.AfterID, query.Limit
	if limit < 1 || limit > 101 {
		return nil, application.ErrInvalid
	}
	arguments := []any{importID}
	watermark := ""
	if afterID != "" {
		watermark = ` AND (collection.metadata_relative_path>? OR
(collection.metadata_relative_path=? AND collection.segment_ordinal>?) OR
(collection.metadata_relative_path=? AND collection.segment_ordinal=? AND collection.id>?))`
		arguments = append(arguments, afterPath, afterPath, afterOrdinal, afterPath, afterOrdinal, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(ctx, `
SELECT collection.id,collection.metadata_relative_path,collection.segment_ordinal,collection.name,
collection.shortname,collection.description,collection.game_count,collection.issue_count,collection.mapping_action,
collection.target_platform_instance_id,platform.name,collection.target_default_core_id,core.name,
collection.ignored_rules_json,collection.warning_fields_json,collection.tag_snapshot_json,plan.state
FROM pegasus_import_collections collection
JOIN pegasus_imports plan ON plan.id=collection.import_id
LEFT JOIN platform_instances platform ON platform.id=collection.target_platform_instance_id
LEFT JOIN cores core ON core.id=collection.target_default_core_id
WHERE collection.import_id=?`+watermark+`
ORDER BY collection.metadata_relative_path,collection.segment_ordinal,collection.id LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/list collections: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]application.CollectionRecord, 0)
	for rows.Next() {
		var value application.CollectionRecord
		var shortName, action, platformID, platformName, coreID, coreName sql.NullString
		var importState string
		var ignored, warnings, tagSnapshot string
		if err := rows.Scan(&value.ID, &value.MetadataRelativePath, &value.SegmentOrdinal, &value.Name, &shortName,
			&value.Description, &value.GameCount, &value.IssueCount, &action, &platformID, &platformName, &coreID,
			&coreName, &ignored, &warnings, &tagSnapshot, &importState); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan collection: %w", err)
		}
		value.ShortName, value.MappingAction = nullableString(shortName), nullableString(action)
		value.TargetPlatformInstanceID, value.TargetPlatformInstanceName = nullableString(
			platformID,
		), nullableString(
			platformName,
		)
		value.TargetDefaultCoreID, value.TargetDefaultCoreName = nullableString(coreID), nullableString(coreName)
		if err := decodeArray(ignored, &value.IgnoredRules); err != nil {
			return nil, fmt.Errorf("decode ignored rules: %w", err)
		}
		if err := decodeArray(warnings, &value.WarningFields); err != nil {
			return nil, fmt.Errorf("decode warning fields: %w", err)
		}
		if err := decodeArray(tagSnapshot, &value.TagSnapshot); err != nil {
			return nil, fmt.Errorf("pegasusimport/decode collection tag snapshot: %w", err)
		}
		value.ImportState = importState
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate collections: %w", err)
	}
	return result, nil
}
