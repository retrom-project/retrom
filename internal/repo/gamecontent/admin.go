package gamecontent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/capability/content/gametitle"
	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/gamecontent"
	"retrom/internal/repo/recordstore"

	"github.com/google/uuid"
)

func (records records) AdminGame(ctx context.Context, gameID string) (application.AdminGameDetail, error) {
	var result application.AdminGameDetail
	var payloadReleaseJobID, payloadLastErrorCode sql.NullString
	var players, releaseYear, deletedAtMS sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `
SELECT g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year,
g.status,
g.payload_state,
g.payload_release_job_id,
g.payload_last_error_code,
pi.id,
pi.name,
pi.platform_id,
g.content_kind,
g.version,
g.created_at_ms,
g.updated_at_ms,
g.deleted_at_ms
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.id=?
`, gameID).Scan(
		&result.Title,
		&result.Description,
		&result.Developer,
		&result.Publisher,
		&result.Genre,
		&players,
		&releaseYear,
		&result.Status,
		&result.PayloadState,
		&payloadReleaseJobID,
		&payloadLastErrorCode,
		&result.InstanceID,
		&result.InstanceName,
		&result.PlatformID,
		&result.ContentKind,
		&result.Version,
		&result.CreatedAtMS,
		&result.UpdatedAtMS,
		&deletedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.AdminGameDetail{}, application.ErrAdminGameNotFound
	}
	if err != nil {
		return application.AdminGameDetail{}, fmt.Errorf("read admin game: %w", err)
	}
	result.Players = nullableInt64Pointer(players)
	result.ReleaseYear = nullableInt64Pointer(releaseYear)
	result.PayloadReleaseJobID = nullableStringPointer(payloadReleaseJobID)
	result.PayloadLastErrorCode = nullableStringPointer(payloadLastErrorCode)
	result.DeletedAtMS = nullableInt64Pointer(deletedAtMS)
	result.Files, err = records.adminGameFiles(ctx, gameID)
	if err != nil {
		return application.AdminGameDetail{}, err
	}
	result.Assets, err = records.adminGameAssets(ctx, gameID)
	if err != nil {
		return application.AdminGameDetail{}, err
	}
	result.Variants, err = records.adminGameVariants(ctx, gameID)
	if err != nil {
		return application.AdminGameDetail{}, err
	}
	return result, nil
}

func (records records) adminGameFiles(ctx context.Context, gameID string) ([]application.AdminGameFile, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.sort_order,blob.size_bytes,blob.sha256
FROM game_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_id=?
ORDER BY file.sort_order,file.role,file.logical_name
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game files: %w", err)
	}
	defer func() { cleanup.Error("close admin game files", rows.Close()) }()
	result := make([]application.AdminGameFile, 0)
	for rows.Next() {
		var file application.AdminGameFile
		if err := rows.Scan(&file.Role, &file.LogicalName, &file.SortOrder, &file.SizeBytes, &file.SHA256); err != nil {
			return nil, fmt.Errorf("scan admin game file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game files: %w", err)
	}
	return result, nil
}

func (records records) adminGameAssets(ctx context.Context, gameID string) ([]application.AdminGameAsset, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT id,kind,ordinal,width_px,height_px,media_type
FROM game_assets
WHERE game_id=?
ORDER BY kind,ordinal,id
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game assets: %w", err)
	}
	defer func() { cleanup.Error("close admin game assets", rows.Close()) }()
	result := make([]application.AdminGameAsset, 0)
	for rows.Next() {
		var asset application.AdminGameAsset
		var width, height sql.NullInt64
		if err := rows.Scan(&asset.ID, &asset.Kind, &asset.Ordinal, &width, &height, &asset.MediaType); err != nil {
			return nil, fmt.Errorf("scan admin game asset: %w", err)
		}
		asset.WidthPX = nullableInt64Pointer(width)
		asset.HeightPX = nullableInt64Pointer(height)
		result = append(result, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game assets: %w", err)
	}
	return result, nil
}

func (records records) adminGameVariants(ctx context.Context, gameID string) ([]application.AdminGameVariant, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT variant.id,variant.core_id,core.name,variant.provider_id,variant.target_id,
 variant.dat_version_id,variant.status,variant.compatibility_code,variant.dependency_snapshot_json,
 variant.version,variant.created_at_ms,variant.updated_at_ms
FROM game_variants variant
JOIN cores core ON core.id=variant.core_id
WHERE variant.game_id=?
ORDER BY core.name,variant.id
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query admin game variants: %w", err)
	}
	defer func() { cleanup.Error("close admin game variants", rows.Close()) }()
	result := make([]application.AdminGameVariant, 0)
	for rows.Next() {
		var variant application.AdminGameVariant
		var providerID, targetID, datVersionID sql.NullString
		var dependencyJSON string
		if err := rows.Scan(
			&variant.ID, &variant.CoreID, &variant.CoreName, &providerID, &targetID, &datVersionID,
			&variant.Status, &variant.CompatibilityCode, &dependencyJSON,
			&variant.Version, &variant.CreatedAtMS, &variant.UpdatedAtMS,
		); err != nil {
			return nil, fmt.Errorf("scan admin game variant: %w", err)
		}
		variant.DependencySnapshot = make(map[string]any)
		if err := json.Unmarshal([]byte(dependencyJSON), &variant.DependencySnapshot); err != nil {
			return nil, fmt.Errorf("decode admin game variant dependency snapshot: %w", err)
		}
		variant.ProviderID = nullableStringPointer(providerID)
		variant.TargetID = nullableStringPointer(targetID)
		variant.DATVersionID = nullableStringPointer(datVersionID)
		result = append(result, variant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin game variants: %w", err)
	}
	return result, nil
}

func (writes writes) LoadPatchState(
	ctx context.Context, gameID string,
) (application.AdminGamePatchState, error) {
	var state application.AdminGamePatchState
	var players, releaseYear sql.NullInt64
	err := writes.transaction.QueryRowContext(ctx, `
SELECT g.status,
g.version,
g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year
FROM games g
WHERE g.id=?
`, gameID).Scan(
		&state.Status,
		&state.Version,
		&state.Metadata.Title,
		&state.Metadata.Description,
		&state.Metadata.Developer,
		&state.Metadata.Publisher,
		&state.Metadata.Genre,
		&players,
		&releaseYear,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.AdminGamePatchState{}, application.ErrAdminGameNotFound
	}
	if err != nil {
		return application.AdminGamePatchState{}, fmt.Errorf("load patch game state: %w", err)
	}
	state.Metadata.Players = nullableInt64Pointer(players)
	state.Metadata.ReleaseYear = nullableInt64Pointer(releaseYear)
	return state, nil
}

func (writes writes) UpdatePatch(ctx context.Context, update application.AdminGamePatchUpdate) (bool, error) {
	result, err := recordstore.UpdateGames(ctx, writes.transaction, recordstore.Update{
		Set: `
title=?,title_initial=?,description=?,developer=?,publisher=?,genre=?,players=?,release_year=?,
metadata_source_kind='ADMIN_EDIT',metadata_source_ref_id=NULL,search_text=?,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=?
AND version=?
`,
			Args: []any{update.GameID, update.ExpectedVersion},
		},
		Values: []any{
			update.Metadata.Title,
			gametitle.Initial(update.Metadata.Title),
			update.Metadata.Description,
			update.Metadata.Developer,
			update.Metadata.Publisher,
			update.Metadata.Genre,
			nullableInt64(update.Metadata.Players),
			nullableInt64(update.Metadata.ReleaseYear),
			searchText(update.Metadata),
			update.NowMS,
		},
	})
	if err != nil {
		return false, fmt.Errorf("update admin game metadata: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count admin game metadata update: %w", err)
	}
	if changed != 1 {
		return false, nil
	}
	if err := recordAdminGameAudit(ctx, writes.transaction, update); err != nil {
		return false, err
	}
	return true, nil
}

func recordAdminGameAudit(ctx context.Context, transaction *sql.Tx, update application.AdminGamePatchUpdate) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create admin game audit identity: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,
actor_kind,
actor_user_id,
actor_label,
action,
resource_type,
resource_id,
before_json,
after_json,
diff_json,
request_id,
created_at_ms) VALUES(?,
?,
?,
?,
'GAME_METADATA_UPDATED',
'GAME',
?,
?,
?,
'{}',
?,
?)
`, id.String(), update.Actor.Kind, update.Actor.UserID, update.Actor.Label, update.GameID,
		fmt.Sprintf(`{"version":%d}`, update.ExpectedVersion),
		fmt.Sprintf(`{"version":%d}`, update.ExpectedVersion+1), nullableRequestID(update.Actor.RequestID), update.NowMS)
	if err != nil {
		return fmt.Errorf("insert admin game audit event: %w", err)
	}
	return nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func searchText(metadata application.AdminGameMetadata) string {
	return strings.ToLower(strings.Join([]string{
		metadata.Title, metadata.Developer, metadata.Publisher, metadata.Genre,
	}, " "))
}

func nullableRequestID(value any) any {
	if value == nil {
		return nil
	}
	if text, ok := value.(string); ok && text == "" {
		return nil
	}
	return value
}

var (
	_ application.AdminGameReader = records{}
	_ application.AdminGameWriter = writes{}
)
