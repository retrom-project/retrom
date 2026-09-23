package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/profilemodel"
	application "retrom/internal/service/libraryimport"
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
	var raw string
	if err := records.transaction.QueryRowContext(ctx, `
SELECT review_profile_json FROM import_items WHERE id=? AND review_profile_json IS NOT NULL`, source.DraftID,
	).Scan(&raw); err != nil {
		return fmt.Errorf("read approved RPG profile: %w", err)
	}
	decoded, err := profilemodel.Decode(profilemodel.Review, raw)
	if err != nil {
		return fmt.Errorf("decode approved RPG profile: %w", err)
	}
	review, ok := decoded.(*profilemodel.RPGReview)
	if !ok {
		return fmt.Errorf("%w: approved review model %T", profilemodel.ErrInvalidModel, decoded)
	}
	profile, err := profilemodel.Encode(profilemodel.Game, profilemodel.RPGMakerProject, review.Game())
	if err != nil {
		return fmt.Errorf("encode approved RPG profile: %w", err)
	}
	result, err := records.transaction.ExecContext(ctx, `
UPDATE games SET content_profile_json=? WHERE id=? AND content_kind='RPG_MAKER_PROJECT'
 AND json_extract(source_manifest_json,'$.fileCount')=?
 AND json_extract(source_manifest_json,'$.totalBytes')=?
 AND json_extract(source_manifest_json,'$.filesDigest')=?`,
		profile, source.GameID, review.FileCount, review.TotalBytes, review.ProjectFingerprint)
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
