package payloadbios

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/retirementops"
	application "retrom/internal/service/payloadrelease"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) BIOS(ctx context.Context, limit int) (application.BIOSRetirement, error) {
	var facts application.BIOSRetirement
	err := dbapi.QueryRowContext(ctx, records.Executor, `SELECT id,blob_id,version,
EXISTS(SELECT 1 FROM bios_installations active WHERE active.blob_id=retired.blob_id AND active.is_active=1)
FROM bios_installations retired WHERE is_active=0 AND blob_id IS NOT NULL ORDER BY updated_at_ms,id LIMIT 1`).
		Scan(&facts.ID, &facts.BlobID, &facts.Version, &facts.SharedActive)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return facts, fmt.Errorf("read retired installation: %w", err)
	}
	facts.Found = true
	if !facts.SharedActive {
		facts.Files, err = retirementops.Files(records.Executor, ctx, `SELECT game_variant_id,logical_name,blob_id FROM variant_files
WHERE role='BIOS_BUNDLE' AND blob_id=? ORDER BY game_variant_id,logical_name LIMIT ?`, facts.BlobID, limit)
		if err != nil {
			return application.BIOSRetirement{}, err
		}
	}
	return facts, nil
}

func (records Records) FenceBIOS(ctx context.Context, before application.BIOSRetirement) error {
	result, err := records.Executor.ExecContext(ctx, `UPDATE bios_installations SET version=version
WHERE id=? AND version=? AND blob_id=? AND is_active=0 AND
EXISTS(SELECT 1 FROM bios_installations active WHERE active.blob_id=? AND active.is_active=1)=?`,
		before.ID, before.Version, before.BlobID, before.BlobID, before.SharedActive)
	if err := retirementops.Write(result, err, 1); err != nil {
		return fmt.Errorf("fence BIOS retirement: %w", err)
	}
	return nil
}

func (records Records) ReleaseBIOSFiles(ctx context.Context, before application.BIOSRetirement) error {
	return retirementops.DeleteFiles(ctx, records.Executor, recordstore.DeleteVariantFiles,
		`role='BIOS_BUNDLE' AND (game_variant_id,logical_name,blob_id)`, before.Files)
}

func (records Records) CompleteBIOS(ctx context.Context, before application.BIOSRetirement, now int64) error {
	result, err := recordstore.UpdateBiosInstallations(ctx, records.Executor, recordstore.Update{
		Set: `blob_id=NULL,payload_released_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND blob_id=? AND is_active=0 AND (
EXISTS(SELECT 1 FROM bios_installations active WHERE active.blob_id=? AND active.is_active=1)
OR NOT EXISTS(SELECT 1 FROM variant_files WHERE role='BIOS_BUNDLE' AND blob_id=?))`,
			Args: []any{before.ID, before.Version, before.BlobID, before.BlobID, before.BlobID},
		}, Values: []any{now, now},
	})
	if err := retirementops.Write(result, err, 1); err != nil {
		return fmt.Errorf("release retired BIOS owner: %w", err)
	}
	return nil
}
