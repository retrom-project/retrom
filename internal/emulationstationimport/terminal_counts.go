package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

type terminalItemCounts = application.TerminalItemCounts

func loadTerminalItemCounts(ctx context.Context, tx *sql.Tx, id string) (terminalItemCounts, error) {
	value, err := repository.LoadTerminalItemCounts(ctx, tx, id)
	if err != nil {
		return terminalItemCounts{}, fmt.Errorf("read EmulationStation terminal counts: %w", err)
	}
	return value, nil
}
