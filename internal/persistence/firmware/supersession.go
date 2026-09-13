package firmware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/payloadrelease"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/firmware"
)

type supersessionRecords struct{ executor dbexec.Executor }

func BindSupersession(executor dbexec.Executor) firmware.SupersessionScope {
	records := supersessionRecords{executor: executor}
	return firmware.SupersessionScope{Read: records, Write: records, Payload: payloadrelease.BindScheduling(executor)}
}

func (records supersessionRecords) Current(
	ctx context.Context, requirementID string,
) (firmware.SupersededInstallation, bool, error) {
	var result firmware.SupersededInstallation
	err := records.executor.QueryRowContext(ctx, `SELECT id,requirement_id,blob_id,version
FROM bios_installations WHERE requirement_id=? AND is_active=1`, requirementID).
		Scan(&result.ID, &result.RequirementID, &result.BlobID, &result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return firmware.SupersededInstallation{}, false, nil
	}
	if err != nil {
		return firmware.SupersededInstallation{}, false, fmt.Errorf("read current BIOS installation: %w", err)
	}
	return result, true, nil
}

func (records supersessionRecords) Consumption(ctx context.Context, installationID string) (string, error) {
	var id string
	err := records.executor.QueryRowContext(ctx, `SELECT id FROM upload_consumptions
WHERE consumer_type='BIOS_INSTALLATION' AND consumer_id=? AND released_at_ms IS NULL`, installationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read BIOS upload consumption: %w", err)
	}
	return id, nil
}

func (records supersessionRecords) Deactivate(
	ctx context.Context, before firmware.SupersededInstallation, now int64,
) error {
	return changed(recordstore.UpdateBiosInstallations(ctx, records.executor, recordstore.Update{
		Set: `is_active=0,version=version+1,updated_at_ms=?`, Values: []any{now},
		Scope: recordstore.Scope{
			Where: `id=? AND requirement_id=? AND blob_id=? AND version=? AND is_active=1`,
			Args:  []any{before.ID, before.RequirementID, before.BlobID, before.Version},
		},
	}))
}
