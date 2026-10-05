package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/filestore"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/profilemodel"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) CreateGame(ctx context.Context, game libraryservice.ApprovalGame) error {
	m := game.Metadata
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,
release_year,
 metadata_source_kind,content_kind,content_source_kind,
 source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,
updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'PUBLISHED',?,1,?,?)`,
		game.ID, game.PlatformInstanceID, m.Title, game.TitleInitial, m.Description, m.Developer,
		m.Publisher, m.Genre, m.Players, m.ReleaseYear,
		game.SourceKind, game.ContentKind, game.SourceKind,
		game.ManifestJSON, game.ManifestDigest, game.SearchText, game.NowMS, game.NowMS)
	return approvalMutation(result, err, "insert approved game", true)
}

func (records reviewApprovalRecords) CopySourceFiles(
	ctx context.Context, source libraryservice.ApprovalContentCopy,
) error {
	files, err := records.publishedSourceFiles(ctx, source)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("encode approved files: %w", err)
	}
	result, err := recordstore.CreateGameFiles(ctx, records.transaction, `
INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order)
SELECT ?,file.role,file.logical_name,file.file_record,file.sort_order
FROM jsonb_to_recordset(?::jsonb) AS file(role text,logical_name text,file_record text,sort_order bigint)
ORDER BY file.sort_order,file.role,file.logical_name`, source.GameID, string(encoded))
	return approvalMutation(result, err, "copy approved source files", false)
}

// File records are serialized by filestore for every owner. Reformatting their
// JSON in SQL would detach archive indexes whose identity is the exact record.
type publishedSourceFile struct {
	Role        string `json:"role"`
	LogicalName string `json:"logical_name"`
	FileRecord  string `json:"file_record"`
	SortOrder   int64  `json:"sort_order"`
}

func (records reviewApprovalRecords) publishedSourceFiles(ctx context.Context,
	source libraryservice.ApprovalContentCopy,
) ([]publishedSourceFile, error) {
	rows, err := records.transaction.QueryContext(ctx, `
SELECT role,logical_name,file_record,sort_order FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? ORDER BY sort_order,role,logical_name`, source.SnapshotID)
	if err != nil {
		return nil, fmt.Errorf("read approved files: %w", err)
	}
	defer func() { cleanup.Error("close approved files", rows.Close()) }()
	files := make([]publishedSourceFile, 0)
	for rows.Next() {
		var file publishedSourceFile
		if err := rows.Scan(&file.Role, &file.LogicalName, &file.FileRecord, &file.SortOrder); err != nil {
			return nil, fmt.Errorf("scan approved file: %w", err)
		}
		file.FileRecord, err = filestore.PublishedRecord(file.FileRecord, source.ItemID, source.GameID)
		if err != nil {
			return nil, fmt.Errorf("publish approved file record: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approved files: %w", err)
	}
	return files, nil
}

func (records reviewApprovalRecords) CopyRPGProfile(
	ctx context.Context,
	source libraryservice.ApprovalContentCopy,
) error {
	var raw string
	if err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT review_profile_json FROM import_items WHERE id=? AND review_profile_json IS NOT
NULL`, source.DraftID,
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
 AND (((source_manifest_json)::jsonb #>> '{fileCount}'))::bigint=?
 AND (((source_manifest_json)::jsonb #>> '{totalBytes}'))::bigint=?
 AND ((source_manifest_json)::jsonb #>> '{filesDigest}')=?`,
		profile, source.GameID, review.FileCount, review.TotalBytes, review.ProjectFingerprint)
	return approvalMutation(result, err, "copy approved RPG profile", true)
}

func (records reviewApprovalRecords) CreateAsset(ctx context.Context, asset libraryservice.ApprovalAsset) error {
	result, err := recordstore.CreateGameAssets(ctx, records.transaction, `
INSERT INTO game_assets(id,game_id,file_record,kind,ordinal,width_px,height_px,media_type,
created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?)`,
		asset.ID, asset.GameID, asset.FileRecord, asset.Kind, asset.Ordinal, asset.WidthPX, asset.HeightPX,
		asset.MediaType, asset.NowMS)
	return approvalMutation(result, err, "insert approved asset", true)
}

func (records reviewApprovalRecords) CopyDOSEntries(
	ctx context.Context,
	source libraryservice.ApprovalContentCopy,
) error {
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO dos_entries(game_id,normalized_path,original_relative_path,kind,rank,enabled,
direct_launch_safe)
SELECT ?,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe
FROM import_item_dos_entries WHERE import_item_id=?`, source.GameID, source.ItemID)
	return approvalMutation(result, err, "copy approved DOS entries", false)
}
