package maintenance

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	pegasus "retrom/internal/persistence/pegasusimport"
)

func clearRestoredPegasusScans(ctx context.Context, tx *sql.Tx) error {
	ids, err := restoredPegasusScanIDs(ctx, tx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pegasus.ClearUnpublishedScan(ctx, tx, id); err != nil {
			return fmt.Errorf("clear restored Pegasus staging: %w", err)
		}
	}
	return nil
}

func restoredPegasusScanIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM pegasus_imports
WHERE state IN ('SCANNING','CANCEL_REQUESTED') AND import_job_id IS NULL AND scan_completed_at_ms IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query restored Pegasus scan plans: %w", err)
	}
	defer func() { cleanup.Error("close restored Pegasus scan plans", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read restored Pegasus scan ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate restored Pegasus scan plans: %w", err)
	}
	return ids, nil
}
