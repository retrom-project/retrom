package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/payloadrelease"
	repository "retrom/internal/persistence/pegasusimport"
)

func scheduleTerminalItems(ctx context.Context, transaction *sql.Tx, id string, now int64) error {
	if err := repository.ScheduleTerminalItems(ctx, transaction, id, now); err != nil {
		return fmt.Errorf("schedule terminal Pegasus payloads: %w", err)
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
