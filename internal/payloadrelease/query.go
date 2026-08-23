package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

func collectIDs(ctx context.Context, transaction *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("payloadrelease/collect ids: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var result []string
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("payloadrelease/scan id: %w", err)
		}
		if id.Valid {
			result = append(result, id.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("payloadrelease/iterate ids: %w", err)
	}
	return result, nil
}

// CollectScopeIDs gives terminal transition owners the same disciplined rows
// lifecycle used by the release worker without duplicating SQL iteration.
func CollectScopeIDs(ctx context.Context, transaction *sql.Tx, query string, args ...any) ([]string, error) {
	return collectIDs(ctx, transaction, query, args...)
}

func execBatches(ctx context.Context, transaction *sql.Tx, query string, args ...any) error {
	for {
		result, err := transaction.ExecContext(ctx, query, args...)
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

func terminalImportItem(state string) bool {
	switch state {
	case "PUBLISHED", "DISCARDED", "FAILED_FINAL", "CANCELLED":
		return true
	default:
		return false
	}
}

func terminalPegasusItem(state string, retryable bool) bool {
	switch state {
	case "PUBLISHED", "REVIEW_DISCARDED", "SKIPPED_EXISTING", "SKIPPED_MAPPING",
		"BLOCKED_SOURCE", "BLOCKED_CONTENT", "CANCELLED":
		return true
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED":
		return !retryable
	default:
		return false
	}
}

func terminalEmulationStationItem(state string, retryable bool) bool {
	return terminalPegasusItem(state, retryable)
}
