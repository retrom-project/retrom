package libraryimport

import (
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/gametitle"
)

func (run *approvalRun) persistGame() error {
	if err := run.insertGame(); err != nil {
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
		run.ctx, run.transaction, run.itemID, run.gameID,
		run.coverID, run.uploadedCoverID, run.backgroundID, run.now,
	)
	if err != nil {
		return err
	}
	if err := run.service.copyExternalAssets(
		run.ctx, run.transaction, run.gameID,
		run.input.decision.ExternalAssets, run.now,
	); err != nil {
		return err
	}
	if err := run.copyContentFiles(); err != nil {
		return err
	}
	return run.copyRPGMakerContentProfile()
}

func (run *approvalRun) copyRPGMakerContentProfile() error {
	if run.platformID != "rpgmaker" {
		return nil
	}
	result, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO rpgmaker_game_profiles(
  game_id,evidence_family,evidence_generation,evidence_confidence,engine_version,
  entry_html_path,file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,
  created_at_ms,updated_at_ms
)
SELECT ?,evidence_family,evidence_generation,evidence_confidence,engine_version,entry_html_path,
  file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,?,?
FROM rpgmaker_review_profiles WHERE review_draft_id=?
`, run.gameID, run.now, run.now, run.draftID)
	if err != nil {
		return fmt.Errorf("libraryimport/rpgmaker content profile: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrInvalid
	}
	return nil
}

func (run *approvalRun) insertGame() error {
	metadata := run.metadata
	title := strings.TrimSpace(metadata.Title)
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO games(
  id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
  source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'PUBLISHED',?,1,?,?)
`, run.gameID, run.platformInstanceID, title, gametitle.Initial(title), metadata.Description,
		metadata.Developer, metadata.Publisher, metadata.Genre, metadata.Players, metadata.ReleaseYear,
		run.input.metadataSourceKind, run.input.metadataSourceRefID, run.contentKind,
		run.input.metadataSourceKind, run.input.metadataSourceRefID,
		run.sourceManifestJSON, run.sourceManifestDigest, strings.ToLower(title), run.now, run.now)
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
INSERT INTO game_files(
  game_id,role,logical_name,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order
) VALUES(?,?,?,?,?,?,?)
`, run.gameID, role, logicalName, blobID, nullable(archiveID), nullableInt(archiveOrdinal), sortOrder)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}
