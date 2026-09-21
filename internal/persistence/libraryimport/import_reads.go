package libraryimport

import (
	"context"
	"database/sql"
	"fmt"
	"path"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

type ImportReads struct {
	executor dbexec.Executor
}

func NewImportReads(database *sql.DB) *ImportReads {
	return BindImportReads(database)
}

func BindImportReads(executor dbexec.Executor) *ImportReads {
	return &ImportReads{executor: executor}
}

const userVisibleImportJobPredicate = `i.id NOT IN (
 SELECT pegasus_item.library_import_job_id FROM pegasus_import_items pegasus_item
 WHERE pegasus_item.library_import_job_id IS NOT NULL
 UNION ALL
 SELECT source_item.library_import_job_id FROM emulationstation_import_items source_item
 WHERE source_item.library_import_job_id IS NOT NULL
 UNION ALL
 SELECT job.id FROM import_jobs job
 JOIN server_import_upload_owners owner ON owner.upload_session_id=job.upload_session_id
)`

const importOverviewSummarySQL = `
WITH ordinary AS (
 SELECT i.state,i.total_item_count,i.failed_item_count,i.rejected_file_count,i.resolved_rejected_file_count
 FROM import_jobs i
 WHERE ` + userVisibleImportJobPredicate + `
)
SELECT
 (SELECT count(*) FROM ordinary WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))+
 (SELECT count(*) FROM pegasus_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED'))+
 (SELECT count(*) FROM emulationstation_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),
 (SELECT count(*)
  FROM import_items item
  WHERE item.state='REVIEW_PENDING'
  AND (
   item.review_handoff_kind='DIRECT'
   OR EXISTS (
    SELECT 1
    FROM emulationstation_import_items source
    WHERE source.library_import_item_id=item.id
    AND source.execution_state='REVIEW_PENDING'
   )
  )),
 (SELECT count(*) FROM import_items WHERE state='PUBLISHED'),
 (SELECT count(*) FROM ordinary WHERE state='COMPLETED')+
 (SELECT count(*) FROM pegasus_imports WHERE state='COMPLETED')+
 (SELECT count(*) FROM emulationstation_imports WHERE state='COMPLETED'),
 (SELECT count(*) FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED'))+
 (SELECT count(*) FROM pegasus_imports WHERE state IN ('PARTIAL_FAILURE','FAILED'))+
 (SELECT count(*) FROM emulationstation_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM pegasus_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM emulationstation_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 COALESCE((SELECT sum(total_item_count) FROM ordinary
  WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')),0)+
 COALESCE((SELECT sum(game_count) FROM pegasus_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),0)+
 COALESCE((SELECT sum(game_count) FROM emulationstation_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),0),
 COALESCE((SELECT sum(failed_item_count+CASE
   WHEN rejected_file_count>resolved_rejected_file_count
   THEN rejected_file_count-resolved_rejected_file_count ELSE 0 END)
  FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)+
 COALESCE((SELECT sum(blocked_item_count+failed_item_count) FROM pegasus_imports
  WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)+
 COALESCE((SELECT sum(blocked_item_count+failed_item_count) FROM emulationstation_imports
  WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)
`

func (repository *ImportReads) Summary(ctx context.Context) (application.ImportOverviewSummary, error) {
	var result application.ImportOverviewSummary
	err := repository.executor.QueryRowContext(ctx, importOverviewSummarySQL).Scan(
		&result.Running,
		&result.ReviewPending,
		&result.PublishedItems,
		&result.Completed,
		&result.Failed,
		&result.OrdinaryFailed,
		&result.PegasusFailed,
		&result.EmulationStationFailed,
		&result.ProcessingItems,
		&result.IssueItems,
	)
	if err != nil {
		return application.ImportOverviewSummary{}, fmt.Errorf("query import overview: %w", err)
	}
	return result, nil
}

const importListSQL = `
SELECT i.id,
i.state,
pi.name,
i.metadata_provider,
coalesce(json_extract(i.config_snapshot_json,'$.contentMode'),'STANDARD'),
i.total_item_count,
i.review_pending_item_count,
i.failed_item_count,
i.rejected_file_count,
i.resolved_rejected_file_count,
i.already_imported_item_count,
i.already_imported_file_count,
i.last_error_code,
i.version,
i.created_at_ms,
i.updated_at_ms
FROM import_jobs i
JOIN platform_instances pi ON pi.id=i.target_platform_instance_id
WHERE ` + userVisibleImportJobPredicate + `
AND (?='' OR instr(lower(i.id),lower(?))>0 OR instr(lower(pi.name),lower(?))>0)
AND (?='' OR i.state=?)
AND (?='' OR i.target_platform_instance_id=?)
AND (?='' OR
(?='UPDATED_DESC' AND (i.updated_at_ms<? OR (i.updated_at_ms=? AND i.id<?))) OR
(?='CREATED_DESC' AND (i.created_at_ms<? OR (i.created_at_ms=? AND i.id<?))))
ORDER BY CASE ? WHEN 'UPDATED_DESC' THEN i.updated_at_ms WHEN 'CREATED_DESC' THEN i.created_at_ms END DESC,
i.id DESC
LIMIT ?
`

func (repository *ImportReads) List(
	ctx context.Context,
	query application.ImportListQuery,
) ([]application.ImportListItem, error) {
	arguments := []any{
		query.QueryText, query.QueryText, query.QueryText,
		query.State, query.State,
		query.PlatformID, query.PlatformID,
		query.CursorID,
		query.SortCode, query.CursorValue, query.CursorValue, query.CursorID,
		query.SortCode, query.CursorValue, query.CursorValue, query.CursorID,
		query.SortCode,
		query.Limit,
	}
	rows, err := repository.executor.QueryContext(ctx, importListSQL, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query imports: %w", err)
	}
	defer func() { cleanup.Error("close import list", rows.Close()) }()
	items := make([]application.ImportListItem, 0, query.Limit)
	for rows.Next() {
		var item application.ImportListItem
		var resolvedRejected int64
		var lastErrorCode sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.State,
			&item.PlatformInstanceName,
			&item.MetadataProvider,
			&item.ContentMode,
			&item.TotalItemCount,
			&item.ReviewPendingItemCount,
			&item.FailedItemCount,
			&item.RejectedFileCount,
			&resolvedRejected,
			&item.AlreadyImportedItemCount,
			&item.AlreadyImportedFileCount,
			&lastErrorCode,
			&item.Version,
			&item.CreatedAtMS,
			&item.UpdatedAtMS,
		); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		item.UnresolvedRejectedFileCount = item.RejectedFileCount - resolvedRejected
		if lastErrorCode.Valid {
			value := lastErrorCode.String
			item.LastErrorCode = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate imports: %w", err)
	}
	return items, nil
}

const importDetailSQL = `
SELECT i.id,
i.upload_session_id,
i.target_platform_instance_id,
p.name,
i.platform_id,
i.default_core_id,
i.provider_id,
i.target_id,
i.dat_version_id,
i.metadata_provider,
i.config_snapshot_json,
i.state,
i.payload_state,
i.payload_release_job_id,
i.total_item_count,
i.queued_item_count,
i.running_item_count,
i.review_pending_item_count,
i.published_item_count,
i.discarded_item_count,
i.failed_item_count,
i.cancelled_item_count,
i.ignored_file_count,
i.rejected_file_count,
i.resolved_rejected_file_count,
i.already_imported_item_count,
i.already_imported_file_count,
i.last_error_code,
i.cancel_reason,
i.reconfigured_from_import_job_id,
i.version,
i.created_at_ms,
i.updated_at_ms
FROM import_jobs i
JOIN platform_instances p ON p.id=i.target_platform_instance_id
WHERE i.id=?
`

func (repository *ImportReads) Detail(
	ctx context.Context,
	importJobID string,
) (application.ImportDetail, error) {
	var result application.ImportDetail
	var configJSON string
	var datID, payloadReleaseJobID, errorCode, cancelReason, reconfiguredFrom sql.NullString
	var rejected, resolvedRejected int64
	err := repository.executor.QueryRowContext(ctx, importDetailSQL, importJobID).Scan(
		&result.ImportJobID,
		&result.UploadID,
		&result.TargetPlatformInstance.ID,
		&result.TargetPlatformInstance.Name,
		&result.PlatformID,
		&result.DefaultCoreID,
		&result.ProviderID,
		&result.TargetID,
		&datID,
		&result.MetadataProvider,
		&configJSON,
		&result.State,
		&result.PayloadState,
		&payloadReleaseJobID,
		&result.Counts.Total,
		&result.Counts.Queued,
		&result.Counts.Running,
		&result.Counts.ReviewPending,
		&result.Counts.Published,
		&result.Counts.Discarded,
		&result.Counts.Failed,
		&result.Counts.Cancelled,
		&result.Counts.IgnoredFiles,
		&rejected,
		&resolvedRejected,
		&result.Counts.AlreadyImportedItems,
		&result.Counts.AlreadyImportedFiles,
		&errorCode,
		&cancelReason,
		&reconfiguredFrom,
		&result.Version,
		&result.CreatedAtMS,
		&result.UpdatedAtMS,
	)
	if err != nil {
		return application.ImportDetail{}, fmt.Errorf("query import detail: %w", err)
	}
	result.DatVersionID = importReadString(datID)
	result.PayloadReleaseJobID = importReadString(payloadReleaseJobID)
	result.ErrorCode = importReadString(errorCode)
	result.CancelReason = importReadString(cancelReason)
	result.ReconfiguredFromImportJobID = importReadString(reconfiguredFrom)
	result.ConfigSnapshot = application.DecodeImportDocument(configJSON)
	result.Counts.RejectedFiles = rejected
	result.Counts.UnresolvedRejectedFiles = rejected - resolvedRejected
	result.FileOutcomes, err = repository.fileOutcomes(ctx, importJobID)
	if err != nil {
		return application.ImportDetail{}, err
	}
	result.AlreadyImportedMatches, err = repository.duplicateMatches(ctx, importJobID)
	if err != nil {
		return application.ImportDetail{}, err
	}
	result.ItemSummaries, err = repository.MultiDiscItemSummaries(ctx, importJobID)
	if err != nil {
		return application.ImportDetail{}, err
	}
	return result, nil
}

func (repository *ImportReads) MultiDiscItemSummaries(
	ctx context.Context,
	importJobID string,
) ([]application.ImportMultiDiscItemSummary, error) {
	rows, err := repository.executor.QueryContext(ctx, `
SELECT item.id,item.state,snapshot.content_kind,playlist.logical_name,upload.relative_path,
count(entry.ordinal),coalesce(sum(entry.state='PRESENT'),0),coalesce(sum(entry.state='MISSING'),0)
FROM import_items item
LEFT JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=COALESCE(
  draft.effective_source_snapshot_id,
  (SELECT initial.id FROM import_item_source_snapshots initial
   WHERE initial.import_item_id=item.id AND initial.created_by='IDENTIFICATION')
)
JOIN import_item_source_snapshot_files playlist ON playlist.source_snapshot_id=snapshot.id
AND playlist.role='PLAYLIST_SOURCE'
JOIN upload_files upload ON upload.id=playlist.upload_file_id
LEFT JOIN import_item_multidisc_entries entry ON entry.source_snapshot_id=snapshot.id
WHERE item.import_job_id=? AND snapshot.content_kind='MULTI_DISC'
GROUP BY item.id,item.state,snapshot.content_kind,playlist.logical_name,upload.relative_path
ORDER BY upload.relative_path,item.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query multi-disc item summaries: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc item summaries", rows.Close()) }()
	summaries := make([]application.ImportMultiDiscItemSummary, 0)
	for rows.Next() {
		var summary application.ImportMultiDiscItemSummary
		if err := rows.Scan(
			&summary.ItemID,
			&summary.State,
			&summary.ContentKind,
			&summary.Playlist,
			&summary.PlaylistPath,
			&summary.DiscCount,
			&summary.PresentDiscCount,
			&summary.MissingDiscCount,
		); err != nil {
			return nil, fmt.Errorf("scan multi-disc item summary: %w", err)
		}
		summary.IgnoredFiles = make([]string, 0)
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc item summaries: %w", err)
	}
	ignoredRows, err := repository.executor.QueryContext(ctx, `
SELECT upload.relative_path
FROM import_job_files outcome
JOIN upload_files upload ON upload.id=outcome.upload_file_id
WHERE outcome.import_job_id=? AND outcome.disposition='IGNORED'
ORDER BY upload.relative_path,upload.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query multi-disc ignored files: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc ignored files", ignoredRows.Close()) }()
	ignoredByDirectory := make(map[string][]string)
	for ignoredRows.Next() {
		var relativePath string
		if err := ignoredRows.Scan(&relativePath); err != nil {
			return nil, fmt.Errorf("scan multi-disc ignored file: %w", err)
		}
		directory := path.Dir(relativePath)
		ignoredByDirectory[directory] = append(ignoredByDirectory[directory], path.Base(relativePath))
	}
	if err := ignoredRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc ignored files: %w", err)
	}
	for index := range summaries {
		ignored := ignoredByDirectory[path.Dir(summaries[index].PlaylistPath)]
		summaries[index].IgnoredFileCount = len(ignored)
		if len(ignored) > 20 {
			ignored = ignored[:20]
		}
		summaries[index].IgnoredFiles = ignored
	}
	return summaries, nil
}

func (repository *ImportReads) fileOutcomes(
	ctx context.Context,
	importJobID string,
) ([]application.ImportFileOutcome, error) {
	rows, err := repository.executor.QueryContext(ctx, `
SELECT u.id,
u.relative_path,
u.declared_size_bytes,
f.disposition,
f.reason_code,
resolution.action,
resolution.replacement_import_job_id,
resolution.created_at_ms,
EXISTS(
  SELECT 1
  FROM import_item_source_files source
  JOIN import_items item ON item.id=source.import_item_id
  JOIN import_item_duplicate_matches duplicate ON duplicate.import_item_id=item.id
  WHERE item.import_job_id=f.import_job_id
  AND source.upload_file_id=f.upload_file_id
)
AND NOT EXISTS(
  SELECT 1
  FROM import_item_source_files source
  JOIN import_items item ON item.id=source.import_item_id
  WHERE item.import_job_id=f.import_job_id
  AND source.upload_file_id=f.upload_file_id
  AND NOT EXISTS(
    SELECT 1 FROM import_item_duplicate_matches duplicate
    WHERE duplicate.import_item_id=item.id
  )
)
FROM import_job_files f
JOIN upload_files u ON u.id=f.upload_file_id
LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id
AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=?
ORDER BY u.relative_path,u.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query import file outcomes: %w", err)
	}
	defer func() { cleanup.Error("close import file outcomes", rows.Close()) }()
	result := make([]application.ImportFileOutcome, 0)
	for rows.Next() {
		var outcome application.ImportFileOutcome
		var reasonCode, resolutionAction, replacementImportJobID sql.NullString
		var resolvedAtMS sql.NullInt64
		var alreadyImported int64
		if err := rows.Scan(
			&outcome.UploadFileID,
			&outcome.Name,
			&outcome.SizeBytes,
			&outcome.Disposition,
			&reasonCode,
			&resolutionAction,
			&replacementImportJobID,
			&resolvedAtMS,
			&alreadyImported,
		); err != nil {
			return nil, fmt.Errorf("scan import file outcome: %w", err)
		}
		outcome.ReasonCode = importReadString(reasonCode)
		if alreadyImported == 1 {
			value := "ALREADY_IMPORTED"
			outcome.Disposition = value
			outcome.ReasonCode = &value
		}
		if resolutionAction.Valid && replacementImportJobID.Valid && resolvedAtMS.Valid {
			outcome.Resolution = &application.ImportFileResolution{
				Action:                 resolutionAction.String,
				ReplacementImportJobID: replacementImportJobID.String,
				ResolvedAtMS:           resolvedAtMS.Int64,
			}
		}
		result = append(result, outcome)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import file outcomes: %w", err)
	}
	return result, nil
}

func (repository *ImportReads) duplicateMatches(
	ctx context.Context,
	importJobID string,
) ([]application.ImportDuplicateMatch, error) {
	rows, err := repository.executor.QueryContext(ctx, `
SELECT match.import_item_id,
match.content_identity_digest,
game.id,
metadata.title,
instance.id,
instance.name
FROM import_item_duplicate_matches match
JOIN import_items item ON item.id=match.import_item_id
JOIN games game ON game.id=match.existing_game_id
JOIN games metadata ON metadata.id=game.id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE item.import_job_id=?
ORDER BY item.created_at_ms,item.id,game.created_at_ms,game.id
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("query import duplicate matches: %w", err)
	}
	defer func() { cleanup.Error("close import duplicate matches", rows.Close()) }()
	result := make([]application.ImportDuplicateMatch, 0)
	for rows.Next() {
		var match application.ImportDuplicateMatch
		if err := rows.Scan(
			&match.ImportItemID,
			&match.ContentIdentityDigest,
			&match.ExistingGame.ID,
			&match.ExistingGame.Title,
			&match.ExistingGame.PlatformInstanceID,
			&match.ExistingGame.PlatformInstanceName,
		); err != nil {
			return nil, fmt.Errorf("scan import duplicate match: %w", err)
		}
		result = append(result, match)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import duplicate matches: %w", err)
	}
	return result, nil
}

const reviewHistorySQL = `
SELECT e.id,
e.import_item_id,
e.event_type,
e.reason,
e.created_at_ms,
COALESCE(json_extract(d.metadata_json,
'$.title'),
''),
i.import_job_id
FROM review_events e
JOIN import_items i ON i.id=e.import_item_id
LEFT JOIN review_drafts d ON d.import_item_id=i.id
WHERE e.event_type IN ('APPROVED',
'DISCARDED')
`

func (repository *ImportReads) ReviewHistory(
	ctx context.Context,
	query application.ReviewHistoryQuery,
) ([]application.ReviewHistoryItem, error) {
	sqlQuery := reviewHistorySQL
	arguments := make([]any, 0, 3)
	if query.QueryText != "" {
		sqlQuery += " AND (instr(lower(COALESCE(json_extract(d.metadata_json,'$.title'),'')),?)>0" +
			" OR instr(i.search_text,?)>0)"
		arguments = append(arguments, query.QueryText, query.QueryText)
	}
	if query.Decision != "" {
		sqlQuery += " AND e.event_type=?"
		arguments = append(arguments, query.Decision)
	}
	sqlQuery += " ORDER BY e.created_at_ms DESC,e.id DESC LIMIT 100"
	rows, err := repository.executor.QueryContext(ctx, sqlQuery, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query review history: %w", err)
	}
	defer func() { cleanup.Error("close review history", rows.Close()) }()
	result := make([]application.ReviewHistoryItem, 0)
	for rows.Next() {
		var item application.ReviewHistoryItem
		var reason sql.NullString
		if err := rows.Scan(
			&item.ReviewEventID,
			&item.ImportItemID,
			&item.Decision,
			&reason,
			&item.CreatedAtMS,
			&item.Title,
			&item.ImportJobID,
		); err != nil {
			return nil, fmt.Errorf("scan review history: %w", err)
		}
		item.Reason = importReadString(reason)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review history: %w", err)
	}
	return result, nil
}

func (repository *ImportReads) ReviewHistoryEvent(
	ctx context.Context,
	eventID string,
) (application.ReviewHistoryEvent, error) {
	var result application.ReviewHistoryEvent
	var actorUserID, actorLabel, reason sql.NullString
	var before, after, diff, config, dat, provider string
	err := repository.executor.QueryRowContext(ctx, `
SELECT id,
import_item_id,
event_type,
actor_kind,
actor_user_id,
actor_label,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
reason,
created_at_ms
FROM review_events
WHERE id=?
AND event_type IN ('APPROVED',
'DISCARDED')
`, eventID).Scan(
		&result.ReviewEventID,
		&result.ImportItemID,
		&result.EventType,
		&result.Actor.Kind,
		&actorUserID,
		&actorLabel,
		&before,
		&after,
		&diff,
		&config,
		&dat,
		&provider,
		&reason,
		&result.CreatedAtMS,
	)
	if err != nil {
		return application.ReviewHistoryEvent{}, fmt.Errorf("query review history event: %w", err)
	}
	result.Actor.UserID = importReadString(actorUserID)
	result.Actor.Label = importReadString(actorLabel)
	result.Before = application.DecodeImportDocument(before)
	result.After = application.DecodeImportDocument(after)
	result.Diff = application.DecodeImportDocument(diff)
	result.ConfigEvidence = application.DecodeImportDocument(config)
	result.DANEvidence = application.DecodeImportDocument(dat)
	result.ProviderEvidence = application.DecodeImportDocument(provider)
	result.Reason = importReadString(reason)
	return result, nil
}

func importReadString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

var _ application.ImportReadRepository = (*ImportReads)(nil)
