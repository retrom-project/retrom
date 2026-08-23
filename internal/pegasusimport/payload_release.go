package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/payloadrelease"
)

func scheduleTerminalItems(ctx context.Context, transaction *sql.Tx, importID string, now int64) error {
	ids, err := payloadrelease.CollectScopeIDs(ctx, transaction, `
SELECT id FROM pegasus_import_items WHERE import_id=? AND payload_state='RETAINED' ORDER BY id
`, importID)
	if err != nil {
		return fmt.Errorf("pegasusimport/list terminal payloads: %w", err)
	}
	for _, id := range ids {
		if _, err := payloadrelease.ScheduleTerminalPegasusItem(ctx, transaction, id, now); err != nil {
			return fmt.Errorf("pegasusimport/schedule terminal payload: %w", err)
		}
	}
	return nil
}

func scheduleAllTerminalItems(ctx context.Context, transaction *sql.Tx, now int64) error {
	importIDs, err := payloadrelease.CollectScopeIDs(ctx, transaction, `
SELECT DISTINCT import_id FROM pegasus_import_items WHERE payload_state='RETAINED' ORDER BY import_id
`)
	if err != nil {
		return fmt.Errorf("pegasusimport/list terminal imports: %w", err)
	}
	for _, importID := range importIDs {
		if err := scheduleTerminalItems(ctx, transaction, importID, now); err != nil {
			return err
		}
	}
	return nil
}
