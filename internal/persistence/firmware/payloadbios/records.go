package payloadbios

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/uploads/receivedfiles"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/fileownership"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/retirementops"
	application "retrom/internal/service/cleanupjobs"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) BIOS(ctx context.Context, limit int) (application.BIOSRetirement, error) {
	var facts application.BIOSRetirement
	err := dbapi.QueryRowContext(ctx, records.Executor, `SELECT id,blob_id,version
FROM bios_installations retired WHERE is_active=0 AND blob_id IS NOT NULL ORDER BY updated_at_ms,
id LIMIT 1`).
		Scan(&facts.ID, &facts.BlobID, &facts.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return facts, fmt.Errorf("read retired installation: %w", err)
	}
	facts.Found = true
	{
		facts.Files, err = retirementops.Files(ctx, records.Executor,
			`SELECT game_variant_id,logical_name,blob_id FROM variant_files
WHERE role='BIOS_BUNDLE' AND blob_id=? ORDER BY game_variant_id,logical_name LIMIT ?`, facts.BlobID, limit)
		if err != nil {
			return application.BIOSRetirement{}, wrapErr(err)
		}
	}
	return facts, nil
}

func (records Records) FenceBIOS(ctx context.Context, before application.BIOSRetirement) error {
	result, err := records.Executor.ExecContext(ctx, `UPDATE bios_installations SET version=version
WHERE id=? AND version=? AND blob_id=? AND is_active=0`,
		before.ID, before.Version, before.BlobID)
	if err := retirementops.Write(result, err, 1); err != nil {
		return fmt.Errorf("fence BIOS retirement: %w", err)
	}
	return nil
}

func (records Records) ReleaseBIOSFiles(ctx context.Context, before application.BIOSRetirement) error {
	return wrapErr(retirementops.DeleteFiles(ctx, records.Executor, recordstore.DeleteVariantFiles,
		`role='BIOS_BUNDLE' AND (game_variant_id,logical_name,blob_id)`, before.Files))
}

func (records Records) CompleteBIOS(ctx context.Context, before application.BIOSRetirement, now int64) error {
	result, err := recordstore.UpdateBiosInstallations(ctx, records.Executor, recordstore.Update{
		Set: `blob_id=NULL,payload_released_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND blob_id=? AND is_active=0 AND NOT EXISTS(SELECT 1 FROM variant_files
WHERE role='BIOS_BUNDLE' AND blob_id=?)`,
			Args: []any{before.ID, before.Version, before.BlobID, before.BlobID},
		}, Values: []any{now, now},
	})
	if err := retirementops.Write(result, err, 1); err != nil {
		return fmt.Errorf("release retired BIOS owner: %w", err)
	}
	if err := fileownership.RetireAll(
		ctx,
		records.Executor,
		fileownership.Owner{Kind: "BIOS_INSTALLATION", ID: before.ID},
		now,
	); err != nil {
		return fmt.Errorf("retire owned file: %w", err)
	}
	if err := receivedfiles.ReleaseRetired(ctx, records.Executor, "BIOS_INSTALLATION", before.ID, now); err != nil {
		return fmt.Errorf("release retired BIOS inputs: %w", err)
	}
	return nil
}
