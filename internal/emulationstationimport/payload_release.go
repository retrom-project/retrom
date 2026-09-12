package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/emulationstationimport"
)

func scheduleTerminalItems(ctx context.Context, transaction *sql.Tx, importID string, now int64) error {
	if err := repository.ScheduleTerminalItems(ctx, transaction, importID, now); err != nil {
		return fmt.Errorf("schedule EmulationStation terminal payloads: %w", err)
	}
	return nil
}
