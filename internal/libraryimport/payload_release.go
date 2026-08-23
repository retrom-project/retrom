package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/payloadrelease"
)

func scheduleTerminalPayloads(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, importID string,
	reason payloadrelease.Reason,
	now int64,
) error {
	if _, err := payloadrelease.ScheduleTerminalImportItem(ctx, transaction, itemID, reason, now); err != nil {
		return fmt.Errorf("libraryimport/schedule item payload release: %w", err)
	}
	pegasusIDs, err := payloadrelease.CollectScopeIDs(ctx, transaction, `
SELECT id FROM pegasus_import_items WHERE library_import_item_id=? ORDER BY id
`, itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/read Pegasus payload owner: %w", err)
	}
	for _, pegasusID := range pegasusIDs {
		if _, err := payloadrelease.ScheduleTerminalPegasusItem(ctx, transaction, pegasusID, now); err != nil {
			return fmt.Errorf("libraryimport/schedule Pegasus payload release: %w", err)
		}
	}
	if _, err := payloadrelease.ScheduleTerminalImportJob(ctx, transaction, importID, now); err != nil {
		return fmt.Errorf("libraryimport/schedule aggregate payload release: %w", err)
	}
	return nil
}
