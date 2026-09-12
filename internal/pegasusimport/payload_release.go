package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/pegasusimport"
)

func scheduleTerminalItems(ctx context.Context, transaction *sql.Tx, id string, now int64) error {
	if err := repository.ScheduleTerminalItems(ctx, transaction, id, now); err != nil {
		return fmt.Errorf("schedule terminal Pegasus payloads: %w", err)
	}
	return nil
}
