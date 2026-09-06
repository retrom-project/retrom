package importdiscard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
)

var errAmbiguousOwner = errors.New("IMPORT_BATCH_DISCARD_OWNER_AMBIGUOUS")

func (service *Service) recoverSourceLinks(ctx context.Context, kind, id string) error {
	table, err := batchTable(kind)
	if err != nil {
		return err
	}
	items := table[:len(table)-1] + "_items"
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("importdiscard/recover ownership: %w", err)
	}
	defer cleanup.Rollback(tx)
	ids, err := payloadrelease.CollectScopeIDs(ctx, tx, `
SELECT id FROM `+items+` WHERE import_id=? AND library_import_job_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM server_import_upload_owners WHERE source_item_id=`+items+`.id)`, id)
	if err != nil {
		return fmt.Errorf("importdiscard/list unlinked sources: %w", err)
	}
	for _, itemID := range ids {
		uploadID := uuid.NewSHA1(
			uuid.NameSpaceOID, []byte("retrom:server-source:v1\x00SERVER_"+kind+"_IMPORT:"+itemID),
		).String()
		var importID string
		err := tx.QueryRowContext(ctx, `
SELECT id FROM import_jobs WHERE upload_session_id=?`,
			uploadID).
			Scan(&importID)
		if errors.Is(err, sql.ErrNoRows) && kind == "PEGASUS" {
			importID, err = legacyPegasusOwner(ctx, tx, itemID)
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("importdiscard/recover child identity: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
SELECT upload_session_id,?,? FROM import_jobs WHERE id=? ON CONFLICT(upload_session_id) DO NOTHING`,
			kind, itemID, importID); err != nil {
			return fmt.Errorf("importdiscard/link recovered import: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("importdiscard/commit recovered ownership: %w", err)
	}
	return nil
}

// Older Pegasus envelopes used random IDs. Recover only a unique internal
// envelope with the same complete file set, target and execution interval.
// Browser upload manifests use a different digest format and are excluded.
func legacyPegasusOwner(ctx context.Context, tx *sql.Tx, itemID string) (string, error) {
	ids, err := payloadrelease.CollectScopeIDs(ctx, tx, `SELECT job.id FROM import_jobs job
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
		return "", fmt.Errorf("importdiscard/list legacy envelopes: %w", err)
	}
	var matches []string
	for _, id := range ids {
		valid, err := internalEnvelope(ctx, tx, id)
		if err != nil {
			return "", err
		}
		if valid {
			matches = append(matches, id)
		}
	}
	if len(matches) > 1 {
		return "", errAmbiguousOwner
	}
	if len(matches) == 0 {
		return "", sql.ErrNoRows
	}
	var owners int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pegasus_import_items candidate
JOIN pegasus_import_collections mapping ON mapping.id=candidate.collection_id
JOIN import_jobs job ON job.id=? JOIN upload_sessions upload ON upload.id=job.upload_session_id
WHERE candidate.library_import_job_id IS NULL AND mapping.target_platform_instance_id=job.target_platform_instance_id
AND job.created_at_ms BETWEEN candidate.created_at_ms AND candidate.completed_at_ms
AND upload.total_files=(SELECT count(*) FROM pegasus_import_item_files WHERE item_id=candidate.id)
AND NOT EXISTS(SELECT 1 FROM upload_files file WHERE file.upload_session_id=upload.id AND NOT EXISTS(
 SELECT 1 FROM pegasus_import_item_files source WHERE source.item_id=candidate.id
 AND source.relative_path=file.relative_path AND source.size_bytes=file.declared_size_bytes))`, matches[0]).
		Scan(&owners); err != nil {
		return "", fmt.Errorf("importdiscard/check legacy owner uniqueness: %w", err)
	}
	if owners != 1 {
		return "", errAmbiguousOwner
	}
	return matches[0], nil
}

func internalEnvelope(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT file.relative_path,file.final_blob_id,file.declared_size_bytes,upload.manifest_digest
FROM import_jobs job JOIN upload_sessions upload ON upload.id=job.upload_session_id
JOIN upload_files file ON file.upload_session_id=upload.id WHERE job.id=? ORDER BY file.relative_path`, id)
	if err != nil {
		return false, fmt.Errorf("importdiscard/check legacy envelope: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var files []libraryimport.ServerSourceFile
	var digest string
	for rows.Next() {
		var file libraryimport.ServerSourceFile
		var blob sql.NullString
		if err := rows.Scan(&file.RelativePath, &blob, &file.SizeBytes, &digest); err != nil {
			return false, fmt.Errorf("importdiscard/read legacy envelope: %w", err)
		}
		if !blob.Valid {
			return false, nil
		}
		file.BlobID = blob.String
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("importdiscard/read legacy files: %w", err)
	}
	manifest, err := json.Marshal(map[string]any{"schemaVersion": 1, "files": files})
	if err != nil {
		return false, fmt.Errorf("importdiscard/encode legacy envelope: %w", err)
	}
	sum := sha256.Sum256(manifest)
	return len(files) > 0 && digest == hex.EncodeToString(sum[:]), nil
}
