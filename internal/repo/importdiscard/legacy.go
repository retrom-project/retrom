package importdiscard

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/importdiscard"
	payloadpersistence "retrom/internal/repo/payloadrelease"
)

func (writes writes) LegacyCandidates(ctx context.Context, itemID string) ([]importdiscard.Envelope, error) {
	ids, err := payloadpersistence.CollectScopeIDs(ctx, writes.transaction, `SELECT job.id FROM import_jobs job
JOIN upload_sessions upload ON upload.id=job.upload_session_id
JOIN pegasus_import_items item ON item.id=?
JOIN pegasus_import_collections collection ON collection.id=item.collection_id
WHERE job.target_platform_instance_id=collection.target_platform_instance_id
AND job.created_at_ms BETWEEN item.created_at_ms AND item.completed_at_ms
AND job.total_item_count=0 AND job.rejected_file_count>0 AND job.payload_state='RETAINED'
AND upload.total_files=(SELECT count(*) FROM pegasus_import_item_files WHERE item_id=item.id)
AND NOT EXISTS(SELECT 1 FROM upload_files file WHERE file.upload_session_id=upload.id AND NOT EXISTS(
 SELECT 1 FROM pegasus_import_item_files source WHERE source.item_id=item.id
 AND source.relative_path=file.relative_path AND source.size_bytes=file.declared_size_bytes))
AND NOT EXISTS(SELECT 1 FROM pegasus_import_items owner WHERE owner.library_import_job_id=job.id)
AND NOT EXISTS(SELECT 1 FROM emulationstation_import_items owner WHERE owner.library_import_job_id=job.id)`, itemID)
	if err != nil {
		return nil, fmt.Errorf("importdiscard/list legacy envelopes: %w", err)
	}
	var result []importdiscard.Envelope
	for _, id := range ids {
		envelope, err := writes.envelope(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, envelope)
	}
	return result, nil
}

func (writes writes) OwnerCount(ctx context.Context, id string) (int64, error) {
	var owners int64
	if err := writes.transaction.QueryRowContext(ctx, `SELECT count(*) FROM pegasus_import_items candidate
JOIN pegasus_import_collections mapping ON mapping.id=candidate.collection_id
JOIN import_jobs job ON job.id=? JOIN upload_sessions upload ON upload.id=job.upload_session_id
WHERE candidate.library_import_job_id IS NULL AND mapping.target_platform_instance_id=job.target_platform_instance_id
AND job.created_at_ms BETWEEN candidate.created_at_ms AND candidate.completed_at_ms
AND upload.total_files=(SELECT count(*) FROM pegasus_import_item_files WHERE item_id=candidate.id)
AND NOT EXISTS(SELECT 1 FROM upload_files file WHERE file.upload_session_id=upload.id AND NOT EXISTS(
 SELECT 1 FROM pegasus_import_item_files source WHERE source.item_id=candidate.id
 AND source.relative_path=file.relative_path AND source.size_bytes=file.declared_size_bytes))`, id).
		Scan(&owners); err != nil {
		return 0, fmt.Errorf("importdiscard/check legacy owner uniqueness: %w", err)
	}
	return owners, nil
}

func (writes writes) envelope(ctx context.Context, id string) (importdiscard.Envelope, error) {
	result := importdiscard.Envelope{ImportID: id, Complete: true}
	rows, err := writes.transaction.QueryContext(ctx, `
SELECT file.relative_path,file.final_blob_id,file.declared_size_bytes,upload.manifest_digest
FROM import_jobs job JOIN upload_sessions upload ON upload.id=job.upload_session_id
JOIN upload_files file ON file.upload_session_id=upload.id WHERE job.id=? ORDER BY file.relative_path`, id)
	if err != nil {
		return result, fmt.Errorf("importdiscard/check legacy envelope: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()

	for rows.Next() {
		var file importdiscard.EnvelopeFile
		var blob sql.NullString
		if err := rows.Scan(&file.RelativePath, &blob, &file.SizeBytes, &result.Digest); err != nil {
			return result, fmt.Errorf("importdiscard/read legacy envelope: %w", err)
		}
		if !blob.Valid {
			result.Complete = false
			continue
		}
		file.BlobID = blob.String
		result.Files = append(result.Files, file)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("importdiscard/read legacy files: %w", err)
	}
	return result, nil
}
