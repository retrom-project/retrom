package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/capability/content/multidisc"
	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/libraryimport"
)

func (records *ReviewDependencies) MultiDiscSource(
	ctx context.Context,
	snapshotID string,
) (application.MultiDiscSource, error) {
	var result application.MultiDiscSource
	err := records.executor.QueryRowContext(ctx, `
SELECT file.logical_name,blob.size_bytes,blob.sha256,
coalesce(json_extract(job.config_snapshot_json,'$.multiDisc.maxDiscs'),?),
coalesce(json_extract(job.config_snapshot_json,'$.multiDisc.maxTotalBytes'),?)
FROM import_item_source_snapshot_files file JOIN blobs blob ON blob.id=file.blob_id
JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
JOIN import_items item ON item.id=snapshot.import_item_id JOIN import_jobs job ON job.id=item.import_job_id
WHERE file.source_snapshot_id=? AND file.role='PLAYLIST_SOURCE'`,
		multidisc.MaxDiscs,
		multidisc.MaxTotalBytes,
		snapshotID).Scan(&result.Playlist.Name,
		&result.Playlist.SizeBytes,
		&result.Playlist.SHA256,
		&result.MaxDiscs,
		&result.MaxTotalBytes)
	if err != nil {
		return application.MultiDiscSource{}, fmt.Errorf("read review playlist: %w", err)
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT entry.ordinal,entry.source_reference,entry.canonical_name,entry.state,
entry.source_logical_name,blob.size_bytes,blob.sha256 FROM import_item_multidisc_entries entry
LEFT JOIN blobs blob ON blob.id=entry.blob_id WHERE entry.source_snapshot_id=? ORDER BY entry.ordinal`, snapshotID)
	if err != nil {
		return application.MultiDiscSource{}, fmt.Errorf("query review discs: %w", err)
	}
	defer func() { cleanup.Error("close review discs", rows.Close()) }()
	result.Entries = make([]application.MultiDiscEntry, 0)
	for rows.Next() {
		var row application.MultiDiscEntry
		if err := rows.Scan(&row.Index,
			&row.SourceReference,
			&row.CanonicalName,
			&row.State,
			&row.LogicalName,
			&row.SizeBytes,
			&row.SHA256); err != nil {
			return application.MultiDiscSource{}, fmt.Errorf("scan review disc: %w", err)
		}
		result.Entries = append(result.Entries, row)
	}
	if err := rows.Err(); err != nil {
		return application.MultiDiscSource{}, fmt.Errorf("iterate review discs: %w", err)
	}
	return result, nil
}

func (records *ReviewDependencies) MultiDiscAttachments(
	ctx context.Context,
	itemID string,
) ([]application.MultiDiscAttachment, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT attachment.id,attachment.state,attachment.error_code,attachment.diagnostics_json,
attachment.job_id,job.state,job.error_retryable,job.version,
attachment.version,attachment.created_at_ms,attachment.updated_at_ms,attachment.finished_at_ms
FROM review_multidisc_attachments attachment JOIN jobs job ON job.id=attachment.job_id
WHERE attachment.import_item_id=? ORDER BY attachment.created_at_ms DESC,attachment.id DESC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query multi-disc attachments: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc attachments", rows.Close()) }()
	result := make([]application.MultiDiscAttachment, 0)
	for rows.Next() {
		var row application.MultiDiscAttachment
		var diagnostics []byte
		if err := rows.Scan(&row.ID,
			&row.State,
			&row.ErrorCode,
			&diagnostics,
			&row.JobID,
			&row.JobState,
			&row.ErrorRetryable,
			&row.JobVersion,
			&row.Version,
			&row.CreatedAtMS,
			&row.UpdatedAtMS,
			&row.FinishedAtMS); err != nil {
			return nil, fmt.Errorf("scan multi-disc attachment: %w", err)
		}
		row.Diagnostics = diagnostics
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc attachments: %w", err)
	}
	return result, nil
}
