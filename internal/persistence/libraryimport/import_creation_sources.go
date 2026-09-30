package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/filestore"

	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records creationRecords) Source(ctx context.Context, change libraryservice.CreationSource) error {
	result, err := records.transaction.ExecContext(
		ctx,
		`
INSERT INTO import_items(id,import_job_id,group_key,state,content_kind,source_manifest_json,
 source_manifest_digest,search_text,version,created_at_ms,updated_at_ms) VALUES(?,?,?,
?,?,?,?,?,1,?,?)`,
		change.ItemID,
		change.ImportID,
		change.GroupKey,
		change.State,
		change.ContentKind,
		change.ManifestJSON,
		change.ManifestDigest,
		change.SearchText,
		change.NowMS,
		change.NowMS,
	)
	if err := creationMutation(result, err, "insert creation item", 1); err != nil {
		return err
	}
	for _, file := range change.Files {
		if file.Payload != nil {
			if _, err := filestore.FileRecord(*file.Payload, "application/octet-stream"); err != nil {
				return fmt.Errorf("import creation sources: %w", err)
			}
		}
		if file.ArchiveOrdinal == nil && file.File.FileRecord != file.FileRecord {
			if err := records.copyArchiveFacts(ctx, file.File.FileRecord, file.FileRecord); err != nil {
				return err
			}
		}

		result, err = recordstore.InsertRows(
			ctx,
			records.transaction,
			"import_item_source_files",
			`
INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,
file_record,
 source_archive_file_record,source_archive_entry_ordinal,sort_order,created_at_ms) VALUES(?,
?,?,?,?,?,?,?,?)`,
			change.ItemID,
			file.Role,
			file.LogicalName,
			file.File.ID,
			file.FileRecord,
			creationNullable(file.ArchiveFileRecord),
			file.ArchiveOrdinal,
			file.Order,
			change.NowMS,
		)
		if err := creationMutation(result, err, "insert creation source file", 1); err != nil {
			return err
		}
	}
	if err := records.sourceSnapshot(ctx, change); err != nil {
		return err
	}
	for _, disc := range change.Discs {
		result, err = recordstore.CreateImportItemMultidiscEntries(
			ctx,
			records.transaction,
			`
INSERT INTO import_item_multidisc_entries(source_snapshot_id,ordinal,source_reference,
normalized_reference,
 canonical_name,state,upload_file_id,file_record,source_logical_name,created_at_ms) VALUES(?,
?,?,?,?,?,?,?,?,?)`,
			change.SnapshotID,
			disc.Ordinal,
			disc.SourceReference,
			disc.NormalizedReference,
			disc.CanonicalName,
			disc.State,
			creationNullable(disc.UploadFileID),
			creationNullable(disc.FileRecord),
			creationNullable(disc.SourceLogicalName),
			change.NowMS,
		)
		if err := creationMutation(result, err, "insert creation disc", 1); err != nil {
			return err
		}
	}
	return nil
}

func (records creationRecords) sourceSnapshot(ctx context.Context, change libraryservice.CreationSource) error {
	result, err := records.transaction.ExecContext(
		ctx,
		`
INSERT INTO import_item_source_snapshots(id,import_item_id,content_kind,source_manifest_json,
 source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,?,?,?,'IDENTIFICATION',?)`,
		change.SnapshotID,
		change.ItemID,
		change.ContentKind,
		change.ManifestJSON,
		change.ManifestDigest,
		change.NowMS,
	)
	if err := creationMutation(result, err, "insert creation snapshot", 1); err != nil {
		return err
	}
	result, err = recordstore.InsertRows(ctx, records.transaction, "import_item_source_snapshot_files", `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
 file_record,
 source_archive_file_record,source_archive_entry_ordinal,sort_order,created_at_ms)
SELECT ?,role,logical_name,upload_file_id,file_record,source_archive_file_record,source_archive_entry_ordinal,
 sort_order,created_at_ms
FROM import_item_source_files WHERE import_item_id=?`, change.SnapshotID, change.ItemID)
	return creationMutation(result, err, "copy creation snapshot files", int64(len(change.Files)))
}

func (records creationRecords) Duplicate(ctx context.Context, change libraryservice.CreationDuplicate) error {
	for _, game := range change.Matches {
		result, err := records.transaction.ExecContext(
			ctx,
			`
INSERT INTO import_item_duplicate_matches(import_item_id,existing_game_id,content_identity_digest,
 detected_stage,created_at_ms)
VALUES(?,?,?,'IDENTIFICATION',?)`,
			change.ItemID,
			game.GameID,
			change.Identity,
			change.NowMS,
		)
		if err := creationMutation(result, err, "insert creation duplicate match", 1); err != nil {
			return err
		}
	}
	result, err := recordstore.UpdateImportItems(
		ctx,
		records.transaction,
		recordstore.Update{
			Set:    `state='DISCARDED',version=version+1,updated_at_ms=?,completed_at_ms=?`,
			Scope:  recordstore.Scope{Where: `id=? AND version=1`, Args: []any{change.ItemID}},
			Values: []any{change.NowMS, change.NowMS},
		},
	)
	return creationMutation(result, err, "discard creation duplicate", 1)
}
