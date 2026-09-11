package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SupersedeBIOS changes the active installation without mutating frozen launches.
// The previous payload stays protected until background retirement releases it.
func SupersedeBIOS(ctx context.Context, transaction *sql.Tx, requirementID string, now int64) error {
	var installationID string
	err := transaction.QueryRowContext(ctx, `SELECT id FROM bios_installations
WHERE requirement_id=? AND is_active=1`, requirementID).Scan(&installationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("payloadrelease/read active BIOS: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE bios_installations SET is_active=0,
version=version+1,updated_at_ms=? WHERE id=? AND is_active=1`, now, installationID); err != nil {
		return fmt.Errorf("payloadrelease/supersede BIOS: %w", err)
	}
	return scheduleBIOSConsumption(ctx, transaction, installationID, now)
}

func scheduleBIOSConsumption(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	now int64,
) error {
	var consumptionID string
	err := transaction.QueryRowContext(ctx, `
SELECT id FROM upload_consumptions
WHERE consumer_type='BIOS_INSTALLATION' AND consumer_id=? AND released_at_ms IS NULL
`, installationID).Scan(&consumptionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("payloadrelease/read BIOS consumption: %w", err)
	}
	if _, err := ScheduleConsumption(ctx, transaction, consumptionID, now); err != nil {
		return fmt.Errorf("payloadrelease/schedule BIOS consumption: %w", err)
	}
	return nil
}
