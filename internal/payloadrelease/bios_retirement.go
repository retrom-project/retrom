package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/recordstore"

	"retrom/internal/cleanup"
)

// Each transaction removes at most 200 stale variant edges, retaining all launch edges.
func (service *Service) releaseSupersededBIOS(ctx context.Context) error {
	for {
		worked, err := service.releaseSupersededBIOSBatch(ctx)
		if err != nil || !worked {
			return err
		}
	}
}

func (service *Service) releaseSupersededBIOSBatch(ctx context.Context) (bool, error) {
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("BIOS retirement transaction: %w", err)
	}
	defer cleanup.Rollback(tx)
	var id, blobID string
	err = tx.QueryRowContext(ctx, `SELECT id,blob_id FROM bios_installations
WHERE is_active=0 AND blob_id IS NOT NULL ORDER BY updated_at_ms,id LIMIT 1`).Scan(&id, &blobID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read retired BIOS: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM variant_files WHERE rowid IN (
 SELECT rowid FROM variant_files WHERE role='BIOS_BUNDLE' AND blob_id=?
 AND NOT EXISTS(SELECT 1 FROM bios_installations WHERE blob_id=? AND is_active=1)
 LIMIT 200
)`, blobID, blobID)
	if err != nil {
		return false, fmt.Errorf("release retired variant BIOS: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count retired variant BIOS: %w", err)
	}
	if count < 200 {
		_, err = recordstore.UpdateBiosInstallations(ctx, tx, recordstore.Update{
			Set: `
blob_id=NULL,payload_released_at_ms=?,
version=version+1,updated_at_ms=?
`,
			Scope: recordstore.Scope{
				Where: `id=? AND is_active=0`,
				Args:  []any{id},
			},
			Values: []any{service.now().UnixMilli(), service.now().UnixMilli()},
		})
		if err != nil {
			return false, fmt.Errorf("release retired BIOS: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit BIOS retirement: %w", err)
	}
	return true, nil
}
