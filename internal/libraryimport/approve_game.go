package libraryimport

import (
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
)

func (run *approvalRun) persistGame() error {
	if err := run.insertGameRevisions(); err != nil {
		return err
	}
	actor := reviewActor(run.ctx)
	actorUserID, _ := actor.UserID.(string)
	publishedTags, err := run.service.tags.CopyDraftTagsToGame(
		run.ctx, run.transaction, run.draftID, run.gameID, actorUserID, run.now,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: publish draft tags: %w", err)
	}
	run.publishedTags = publishedTags
	run.screenshotIDs, err = run.service.copyReviewAssets(
		run.ctx, run.transaction, run.itemID, run.gameID, run.metadataID,
		run.coverID, run.uploadedCoverID, run.backgroundID, run.now,
	)
	if err != nil {
		return err
	}
	if err := run.service.copyExternalAssets(
		run.ctx, run.transaction, run.gameID, run.metadataID,
		run.input.decision.ExternalAssets, run.now,
	); err != nil {
		return err
	}
	return run.copyContentFiles()
}

func (run *approvalRun) insertGameRevisions() error {
	metadata := run.metadata
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`, run.metadataID, run.gameID, strings.TrimSpace(metadata.Title), metadata.Description,
		metadata.Developer, metadata.Publisher, metadata.Genre, metadata.Players, metadata.ReleaseYear,
		run.input.metadataSourceKind, run.input.metadataSourceRefID, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO game_content_revisions(
  id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,
  source_manifest_digest,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)
`, run.contentID, run.gameID, run.contentKind, run.input.metadataSourceKind,
		run.input.metadataSourceRefID, run.sourceManifestJSON, run.sourceManifestDigest, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,'PUBLISHED',?,?,?,1,?,?)
`, run.gameID, run.platformInstanceID, run.metadataID, run.contentID,
		strings.ToLower(metadata.Title), run.now, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) copyContentFiles() error {
	rows, err := run.transaction.QueryContext(run.ctx, `
SELECT role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=?
ORDER BY sort_order,role,logical_name
`, run.sourceSnapshotID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		if err := run.copyContentFile(rows); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) copyContentFile(rows *sql.Rows) error {
	var role, logicalName, blobID string
	var archiveID sql.NullString
	var archiveOrdinal sql.NullInt64
	var sortOrder int64
	if err := rows.Scan(
		&role, &logicalName, &blobID, &archiveID, &archiveOrdinal, &sortOrder,
	); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO game_content_files(
  game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order
) VALUES(?,?,?,?,?,?,?)
`, run.contentID, role, logicalName, blobID, nullable(archiveID), nullableInt(archiveOrdinal), sortOrder)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}
