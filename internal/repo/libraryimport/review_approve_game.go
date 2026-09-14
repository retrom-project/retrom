package libraryimport

import (
	"context"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/recordstore"
)

func (records reviewApprovalRecords) CreateGame(ctx context.Context, game application.ApprovalGame) error {
	m := game.Metadata
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'PUBLISHED',?,1,?,?)`,
		game.ID, game.PlatformInstanceID, m.Title, game.TitleInitial, m.Description, m.Developer,
		m.Publisher, m.Genre, m.Players, m.ReleaseYear,
		game.SourceKind, game.SourceRefID, game.ContentKind, game.SourceKind, game.SourceRefID,
		game.ManifestJSON, game.ManifestDigest, game.SearchText, game.NowMS, game.NowMS)
	return approvalMutation(result, err, "insert approved game", true)
}

func (records reviewApprovalRecords) CopySourceFiles(
	ctx context.Context, source application.ApprovalContentCopy,
) error {
	result, err := recordstore.CreateGameFiles(ctx, records.transaction, `
INSERT INTO game_files(game_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order)
SELECT ?,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM import_item_source_snapshot_files WHERE source_snapshot_id=? ORDER BY sort_order,role,logical_name`,
		source.GameID, source.SnapshotID)
	return approvalMutation(result, err, "copy approved source files", false)
}

func (records reviewApprovalRecords) CopyRPGProfile(ctx context.Context, source application.ApprovalContentCopy) error {
	result, err := recordstore.CreateRpgmakerGameProfiles(ctx, records.transaction, `
INSERT INTO rpgmaker_game_profiles(
 game_id,evidence_family,evidence_generation,evidence_confidence,engine_version,
 entry_html_path,file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,
 created_at_ms,updated_at_ms
)
SELECT ?,evidence_family,evidence_generation,evidence_confidence,engine_version,entry_html_path,
 file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,?,?
FROM rpgmaker_review_profiles WHERE review_draft_id=?`, source.GameID, source.NowMS, source.NowMS, source.DraftID)
	return approvalMutation(result, err, "copy approved RPG profile", true)
}

func (records reviewApprovalRecords) CreateAsset(ctx context.Context, asset application.ApprovalAsset) error {
	result, err := recordstore.CreateGameAssets(ctx, records.transaction, `
INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?)`,
		asset.ID, asset.GameID, asset.BlobID, asset.Kind, asset.Ordinal, asset.WidthPX, asset.HeightPX,
		asset.MediaType, asset.NowMS)
	return approvalMutation(result, err, "insert approved asset", true)
}

func (records reviewApprovalRecords) CopyDOSEntries(ctx context.Context, source application.ApprovalContentCopy) error {
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO dos_entries(game_id,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe)
SELECT ?,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe
FROM import_item_dos_entries WHERE import_item_id=?`, source.GameID, source.ItemID)
	return approvalMutation(result, err, "copy approved DOS entries", false)
}
