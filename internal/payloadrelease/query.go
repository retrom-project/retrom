package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	persistence "retrom/internal/persistence/payloadrelease"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
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

type deletionBatch struct {
	remove func(context.Context, dbexec.Executor, recordstore.Scope) (sql.Result, error)
	where  string
}

func execBatches(ctx context.Context, transaction *sql.Tx, batch deletionBatch, args ...any) error {
	for {
		result, err := batch.remove(ctx, transaction, recordstore.Scope{
			Where: batch.where,
			Args:  args,
		})
		if err != nil {
			return fmt.Errorf("payloadrelease/batch: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("payloadrelease/batch rows: %w", err)
		}
		if count < 200 {
			return nil
		}
	}
}

func terminalImportItem(state string) bool { return application.TerminalImportItem(state) }

func terminalPegasusItem(state string, retryable bool) bool {
	return application.TerminalSourceItem(state, retryable)
}
