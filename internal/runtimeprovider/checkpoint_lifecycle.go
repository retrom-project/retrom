package runtimeprovider

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

func validateCheckpointFormats(
	ctx context.Context, transaction *sql.Tx, providerID string, target targetProjection,
) error {
	readable := make(map[string]bool)
	if target.target.Checkpoint != nil {
		for _, format := range target.target.Checkpoint.ReadFormats {
			readable[format] = true
		}
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT DISTINCT save.checkpoint_format FROM save_states save
JOIN game_variants variant ON variant.game_id=save.game_id
WHERE save.deleted_at_ms IS NULL AND variant.provider_id=? AND variant.target_id=?
`, providerID, target.target.ID)
	if err != nil {
		return fmt.Errorf("read active checkpoint formats: %w", err)
	}
	defer func() { cleanup.Error("close active checkpoint formats", rows.Close()) }()
	for rows.Next() {
		var format string
		if err := rows.Scan(&format); err != nil {
			return fmt.Errorf("scan active checkpoint format: %w", err)
		}
		if !readable[format] {
			return fmt.Errorf("%w: %s/%s %s", ErrProviderCheckpointUnreadable, providerID, target.target.ID, format)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("active checkpoint formats: %w", err)
	}
	return nil
}
