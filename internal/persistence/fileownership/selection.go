package fileownership

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
)

// TransferSelected uses a domain-supplied query of file IDs. The query is not
// inferred from schema relationships and must be inside the domain transaction.
func TransferSelected(ctx context.Context, tx dbapi.Executor, from, to Owner, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("select files for handoff: %w", err)
	}
	defer func() { cleanup.Error("close file handoff selection", rows.Close()) }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("read file handoff selection: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate file handoff selection: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close file handoff selection: %w", err)
	}
	for _, id := range ids {
		if err := Transfer(ctx, tx, id, from, to); err != nil {
			return err
		}
	}
	return nil
}
