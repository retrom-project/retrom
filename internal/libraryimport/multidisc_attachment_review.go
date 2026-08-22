package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/multidisc"
)

func (service *Service) ReviewMultiDisc(
	ctx context.Context,
	itemID string,
) (any, bool, error) {
	var snapshotID, contentKind string
	if err := service.database.QueryRowContext(ctx, `
SELECT snapshot.id,snapshot.content_kind
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&snapshotID, &contentKind); err != nil {
		return nil, false, multiDiscAttachmentStoreError("read review", err)
	}
	if contentKind != multidisc.ContentKind {
		return nil, false, nil
	}
	projection, missingCount, err := service.reviewMultiDiscSourceProjection(ctx, snapshotID)
	if err != nil {
		return nil, false, err
	}
	attachments, active, retryRequired, err := service.reviewMultiDiscAttachments(ctx, itemID)
	if err != nil {
		return nil, false, err
	}
	latest := any(nil)
	if len(attachments) > 0 {
		latest = attachments[0]
	}
	projection["latestAttachment"] = latest
	projection["activeAttachment"] = active
	projection["canAttachMissingDiscs"] = missingCount > 0 && active == nil && !retryRequired
	return projection, true, nil
}

func (service *Service) reviewMultiDiscSourceProjection(
	ctx context.Context,
	snapshotID string,
) (map[string]any, int, error) {
	var playlistName, playlistSHA string
	var playlistSize, maxTotalBytes int64
	var maxDiscs int
	if err := service.database.QueryRowContext(ctx, `
SELECT file.logical_name,blob.size_bytes,blob.sha256,
coalesce(json_extract(job.config_snapshot_json,'$.multiDisc.maxDiscs'),?),
coalesce(json_extract(job.config_snapshot_json,'$.multiDisc.maxTotalBytes'),?)
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
JOIN import_items item ON item.id=snapshot.import_item_id
JOIN import_jobs job ON job.id=item.import_job_id
WHERE file.source_snapshot_id=? AND file.role='PLAYLIST_SOURCE'
	`, multidisc.MaxDiscs, multidisc.MaxTotalBytes, snapshotID).Scan(
		&playlistName, &playlistSize, &playlistSHA, &maxDiscs, &maxTotalBytes,
	); err != nil {
		return nil, 0, multiDiscAttachmentStoreError("read review playlist", err)
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT entry.ordinal,entry.source_reference,entry.canonical_name,entry.state,
entry.source_logical_name,blob.size_bytes,blob.sha256
FROM import_item_multidisc_entries entry
LEFT JOIN blobs blob ON blob.id=entry.blob_id
WHERE entry.source_snapshot_id=? ORDER BY entry.ordinal
	`, snapshotID)
	if err != nil {
		return nil, 0, multiDiscAttachmentStoreError("read review entries", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0, multidisc.MaxDiscs)
	missing := make([]string, 0)
	var totalSize int64
	presentCount := 0
	for rows.Next() {
		var ordinal int
		var reference, canonicalName, state string
		var logicalName, blobSHA sql.NullString
		var size sql.NullInt64
		if err := rows.Scan(
			&ordinal, &reference, &canonicalName, &state, &logicalName, &size, &blobSHA,
		); err != nil {
			return nil, 0, multiDiscAttachmentStoreError("scan review entries", err)
		}
		if size.Valid {
			totalSize += size.Int64
			presentCount++
		}
		if state == "MISSING" {
			missing = append(missing, reference)
		}
		entries = append(entries, map[string]any{
			"index": ordinal, "discIndex": ordinal, "label": fmt.Sprintf("光盘 %d", ordinal+1),
			"sourceReference": reference, "canonicalName": canonicalName, "state": state,
			"logicalName": nullable(logicalName), "sizeBytes": nullableInt(size), "sha256": nullable(blobSHA),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, multiDiscAttachmentStoreError("iterate review entries", err)
	}
	return map[string]any{
		"contentKind": multidisc.ContentKind,
		"playlist": map[string]any{
			"name": playlistName, "sizeBytes": playlistSize, "sha256": playlistSHA,
		},
		"discCount": len(entries), "presentDiscCount": presentCount, "missingDiscCount": len(missing),
		"totalPresentBytes": totalSize, "maxDiscs": maxDiscs, "maxTotalBytes": maxTotalBytes,
		"entries": entries, "missingReferences": missing,
	}, len(missing), nil
}

func (service *Service) reviewMultiDiscAttachments(
	ctx context.Context,
	itemID string,
) ([]map[string]any, any, bool, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT attachment.id,attachment.state,attachment.error_code,attachment.diagnostics_json,
attachment.job_id,job.state,job.error_retryable,job.version,
attachment.version,attachment.created_at_ms,attachment.updated_at_ms,attachment.finished_at_ms
FROM review_multidisc_attachments attachment
JOIN jobs job ON job.id=attachment.job_id
WHERE attachment.import_item_id=? ORDER BY attachment.created_at_ms DESC,attachment.id DESC
`, itemID)
	if err != nil {
		return nil, nil, false, multiDiscAttachmentStoreError("read review attachments", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	attachments := make([]map[string]any, 0)
	var active any
	retryRequired := false
	for rows.Next() {
		var id, state, diagnosticsJSON, jobID, jobState string
		var errorCode sql.NullString
		var retryable sql.NullInt64
		var jobVersion, attachmentVersion, createdAt, updatedAt int64
		var finishedAt sql.NullInt64
		if err := rows.Scan(
			&id, &state, &errorCode, &diagnosticsJSON, &jobID, &jobState, &retryable,
			&jobVersion, &attachmentVersion, &createdAt, &updatedAt, &finishedAt,
		); err != nil {
			return nil, nil, false, multiDiscAttachmentStoreError("scan review attachments", err)
		}
		var diagnostics any
		_ = json.Unmarshal([]byte(diagnosticsJSON), &diagnostics)
		value := map[string]any{
			"attachmentId": id, "state": state, "errorCode": nullable(errorCode),
			"diagnostics": diagnostics, "jobId": jobID, "jobState": jobState,
			"version": attachmentVersion, "jobVersion": jobVersion,
			"canRetry": state == "FAILED_RETRYABLE" &&
				jobState == "FAILED" && retryable.Valid && retryable.Int64 == 1,
			"createdAtMs": createdAt, "updatedAtMs": updatedAt, "finishedAtMs": nullableInt(finishedAt),
		}
		attachments = append(attachments, value)
		activeState := state == "QUEUED" || state == "RUNNING" || state == "FAILED_RETRYABLE"
		if active == nil && activeState && (jobState == "QUEUED" || jobState == "RUNNING") {
			active = value
		}
		if state == "FAILED_RETRYABLE" {
			retryRequired = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, multiDiscAttachmentStoreError("iterate review attachments", err)
	}
	return attachments, active, retryRequired, nil
}
