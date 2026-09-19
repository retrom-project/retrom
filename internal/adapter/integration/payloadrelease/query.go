package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	persistence "retrom/internal/repo/payloadrelease"
)

func collectIDs(ctx context.Context, transaction *sql.Tx, query string, args ...any) ([]string, error) {
	ids, err := persistence.CollectScopeIDs(ctx, transaction, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read payload scope identities: %w", err)
	}
	return ids, nil
}

func CollectScopeIDs(ctx context.Context, transaction *sql.Tx, query string, args ...any) ([]string, error) {
	return collectIDs(ctx, transaction, query, args...)
}

func terminalImportItem(state string) bool { return payloadreleasemodel.TerminalImportItem(state) }

func terminalPegasusItem(state string, retryable bool) bool {
	return payloadreleasemodel.TerminalSourceItem(state, retryable)
}
