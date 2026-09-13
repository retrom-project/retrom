package gameassets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/gameassets"
)

var errReleaseSchedulerUnavailable = errors.New("game asset payload release scheduler unavailable")

type writeScope struct {
	executor dbexec.Executor
	releases ReleaseScheduler
}

func (scope writeScope) GameVersion(ctx context.Context, gameID string) (int64, error) {
	var version int64
	err := scope.executor.QueryRowContext(ctx, `
SELECT g.version
FROM games g
WHERE g.id=?
AND g.status='PUBLISHED'`, gameID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read game asset version: %w", err)
	}
	return version, nil
}

func (scope writeScope) AssetExists(ctx context.Context, gameID, kind string) (bool, error) {
	var exists int
	err := scope.executor.QueryRowContext(ctx, `
SELECT 1
FROM game_assets
WHERE game_id=?
AND kind=?
AND ordinal=0`, gameID, kind).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read game asset: %w", err)
	}
	return exists == 1, nil
}

func (scope writeScope) RemoveSlot(
	ctx context.Context, gameID, kind string, ordinal int64,
) ([]string, error) {
	rows, err := scope.executor.QueryContext(ctx, `
SELECT blob_id FROM game_assets WHERE game_id=? AND kind=? AND ordinal=? ORDER BY id
`, gameID, kind, ordinal)
	if err != nil {
		return nil, fmt.Errorf("list replaced game assets: %w", err)
	}
	defer func() { cleanup.Error("close replaced game assets", rows.Close()) }()
	blobIDs := make([]string, 0, 1)
	for rows.Next() {
		var blobID string
		if err := rows.Scan(&blobID); err != nil {
			return nil, fmt.Errorf("scan replaced game asset: %w", err)
		}
		blobIDs = append(blobIDs, blobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replaced game assets: %w", err)
	}
	if _, err := scope.executor.ExecContext(ctx, `
DELETE FROM game_assets WHERE game_id=? AND kind=? AND ordinal=?
`, gameID, kind, ordinal); err != nil {
		return nil, fmt.Errorf("delete replaced game asset: %w", err)
	}
	return blobIDs, nil
}

func (scope writeScope) Create(ctx context.Context, asset application.AssetRecord) error {
	if _, err := recordstore.CreateGameAssets(ctx, scope.executor, `
INSERT INTO game_assets(id,
game_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,?,?,?,?,?,?,?,?)`,
		asset.ID,
		asset.GameID,
		asset.BlobID,
		asset.Kind,
		asset.Ordinal,
		asset.WidthPX,
		asset.HeightPX,
		asset.MediaType,
		asset.CreatedAtMS,
	); err != nil {
		return fmt.Errorf("insert game asset: %w", err)
	}
	return nil
}

func (scope writeScope) ConsumeUpload(ctx context.Context, record application.ConsumptionRecord) error {
	if _, err := recordstore.CreateUploadConsumptions(ctx, scope.executor, `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,?,?,?,?,?)`,
		record.ID,
		record.UploadID,
		record.UploadFileID,
		"GAME_ASSET",
		record.ConsumerID,
		record.CreatedAtMS,
	); err != nil {
		return fmt.Errorf("consume game asset upload: %w", err)
	}
	return nil
}

func (scope writeScope) UpdateGame(
	ctx context.Context, gameID string, expectedVersion, now int64,
) (bool, error) {
	result, err := recordstore.UpdateGames(ctx, scope.executor, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{gameID, expectedVersion},
		},
		Values: []any{now},
	})
	if err != nil {
		return false, fmt.Errorf("update game asset owner: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count game asset owner update: %w", err)
	}
	return changed == 1, nil
}

func (scope writeScope) StageCandidates(ctx context.Context, ids []string) error {
	if scope.releases == nil {
		return errReleaseSchedulerUnavailable
	}
	if err := scope.releases.StageCandidates(ctx, scope.executor, ids); err != nil {
		return fmt.Errorf("stage game asset payload candidates: %w", err)
	}
	return nil
}

func (scope writeScope) ScheduleConsumption(ctx context.Context, id string, now int64) error {
	if scope.releases == nil {
		return errReleaseSchedulerUnavailable
	}
	if err := scope.releases.ScheduleConsumption(ctx, scope.executor, id, now); err != nil {
		return fmt.Errorf("schedule game asset payload consumption: %w", err)
	}
	return nil
}

var _ application.WriteScope = writeScope{}
