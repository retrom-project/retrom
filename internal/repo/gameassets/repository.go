package gameassets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/gameassets"
	"retrom/internal/repo/dbexec"
)

// ReleaseScheduler keeps payload release work in the same transaction as the
// game asset mutation. The composition layer adapts the running release
// service to this persistence port.
type ReleaseScheduler interface {
	StageCandidates(context.Context, dbexec.Executor, []string) error
	ScheduleConsumption(context.Context, dbexec.Executor, string, int64) error
}

type Repository struct {
	database *sql.DB
	releases ReleaseScheduler
}

func New(database *sql.DB, releases ReleaseScheduler) *Repository {
	return &Repository{database: database, releases: releases}
}

func (repository *Repository) Upload(
	ctx context.Context, uploadFileID string,
) (application.UploadedFile, bool, error) {
	var upload application.UploadedFile
	err := repository.database.QueryRowContext(ctx, `
SELECT f.upload_session_id,
b.id,
b.sha256,
b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
AND f.state='COMPLETE'
`, uploadFileID).Scan(&upload.UploadID, &upload.BlobID, &upload.Digest, &upload.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return application.UploadedFile{}, false, nil
	}
	if err != nil {
		return application.UploadedFile{}, false, fmt.Errorf("read game asset upload: %w", err)
	}
	return upload, true, nil
}

func (repository *Repository) CommitCreate(
	ctx context.Context, cmd application.CreateCommand,
) error {
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		scope := writeScope{executor: exec, releases: repository.releases}
		version, err := scope.GameVersion(ctx, cmd.GameID)
		if err != nil {
			return fmt.Errorf("%w: %w", application.ErrVersionConflict, err)
		}
		if version != cmd.ExpectedVersion {
			return application.ErrVersionConflict
		}
		replaced, err := scope.RemoveSlot(ctx, cmd.GameID, cmd.Kind, cmd.Ordinal)
		if err != nil {
			return fmt.Errorf("remove replaced game asset: %w", err)
		}
		if err := scope.Create(ctx, application.AssetRecord{
			ID: cmd.AssetID, GameID: cmd.GameID, BlobID: cmd.Asset.BlobID,
			Kind: cmd.Kind, Ordinal: cmd.Ordinal,
			WidthPX: cmd.Asset.WidthPX, HeightPX: cmd.Asset.HeightPX,
			MediaType: cmd.Asset.MediaType, CreatedAtMS: cmd.NowMS,
		}); err != nil {
			return fmt.Errorf("create game asset: %w", err)
		}
		if err := scope.ConsumeUpload(ctx, application.ConsumptionRecord{
			ID: cmd.ConsumptionID, UploadID: cmd.Asset.UploadID,
			UploadFileID: cmd.UploadFileID,
			ConsumerID: cmd.AssetID, CreatedAtMS: cmd.NowMS,
		}); err != nil {
			return fmt.Errorf("%w: %w", application.ErrUploadConsumed, err)
		}
		changed, err := scope.UpdateGame(
			ctx, cmd.GameID, cmd.ExpectedVersion, cmd.NowMS,
		)
		if err != nil {
			return fmt.Errorf("update game asset version: %w", err)
		}
		if !changed {
			return application.ErrVersionConflict
		}
		if err := scope.StageCandidates(ctx, replaced); err != nil {
			return fmt.Errorf("stage replaced game assets: %w", err)
		}
		if err := scope.ScheduleConsumption(ctx, cmd.ConsumptionID, cmd.NowMS); err != nil {
			return fmt.Errorf("schedule game asset upload release: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("gameassets: commit create: %w", err)
	}
	return nil
}

func (repository *Repository) CommitDelete(
	ctx context.Context, cmd application.DeleteCommand,
) (application.DeleteResult, error) {
	var result application.DeleteResult
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		scope := writeScope{executor: exec, releases: repository.releases}
		version, err := scope.GameVersion(ctx, cmd.GameID)
		if err != nil {
			return fmt.Errorf("%w: %w", application.ErrVersionConflict, err)
		}
		if version != cmd.ExpectedVersion {
			return application.ErrVersionConflict
		}
		exists, err := scope.AssetExists(ctx, cmd.GameID, cmd.Kind)
		if err != nil {
			return fmt.Errorf("%w: %w", application.ErrAssetNotFound, err)
		}
		if !exists {
			return application.ErrAssetNotFound
		}
		replaced, err := scope.RemoveSlot(ctx, cmd.GameID, cmd.Kind, 0)
		if err != nil {
			return fmt.Errorf("remove game asset: %w", err)
		}
		changed, err := scope.UpdateGame(
			ctx, cmd.GameID, cmd.ExpectedVersion, cmd.NowMS,
		)
		if err != nil {
			return fmt.Errorf("update game asset version: %w", err)
		}
		if !changed {
			return application.ErrVersionConflict
		}
		if err := scope.StageCandidates(ctx, replaced); err != nil {
			return fmt.Errorf("stage deleted game assets: %w", err)
		}
		result.Version = cmd.ExpectedVersion + 1
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("gameassets: commit delete: %w", err)
	}
	return result, nil
}

var _ application.Repository = (*Repository)(nil)
