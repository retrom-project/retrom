package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

func (records retirementRecords) FenceBIOS(ctx context.Context, before application.BIOSRetirement) error {
	result, err := records.executor.ExecContext(ctx, `UPDATE bios_installations SET version=version
WHERE id=? AND version=? AND blob_id=? AND is_active=0 AND
EXISTS(SELECT 1 FROM bios_installations active WHERE active.blob_id=? AND active.is_active=1)=?`,
		before.ID, before.Version, before.BlobID, before.BlobID, before.SharedActive)
	if err := retirementWrite(result, err, 1); err != nil {
		return fmt.Errorf("fence BIOS retirement: %w", err)
	}
	return nil
}

func (records retirementRecords) ReleaseBIOSFiles(ctx context.Context, before application.BIOSRetirement) error {
	return records.deleteFiles(ctx, recordstore.DeleteVariantFiles,
		`role='BIOS_BUNDLE' AND (game_variant_id,logical_name,blob_id)`, before.Files)
}

func (records retirementRecords) CompleteBIOS(ctx context.Context, before application.BIOSRetirement, now int64) error {
	result, err := recordstore.UpdateBiosInstallations(ctx, records.executor, recordstore.Update{
		Set: `blob_id=NULL,payload_released_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND blob_id=? AND is_active=0 AND (
EXISTS(SELECT 1 FROM bios_installations active WHERE active.blob_id=? AND active.is_active=1)
OR NOT EXISTS(SELECT 1 FROM variant_files WHERE role='BIOS_BUNDLE' AND blob_id=?))`,
			Args: []any{before.ID, before.Version, before.BlobID, before.BlobID, before.BlobID},
		}, Values: []any{now, now},
	})
	if err := retirementWrite(result, err, 1); err != nil {
		return fmt.Errorf("release retired BIOS owner: %w", err)
	}
	return nil
}
