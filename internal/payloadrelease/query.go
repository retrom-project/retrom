package payloadrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	persistence "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

func collectIDs(ctx context.Context, transaction dbapi.Tx, query string, args ...any) ([]string, error) {
	ids, err := persistence.CollectScopeIDs(ctx, transaction, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read payload scope identities: %w", err)
	}
	return ids, nil
}

func CollectScopeIDs(ctx context.Context, transaction dbapi.Tx, query string, args ...any) ([]string, error) {
	return collectIDs(ctx, transaction, query, args...)
}

func terminalImportItem(state string) bool { return application.TerminalImportItem(state) }

func terminalSourceItem(state string, retryable bool) bool {
	return application.TerminalSourceItem(state, retryable)
}
