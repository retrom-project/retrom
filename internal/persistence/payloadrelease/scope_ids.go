package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
)

func collectIDs(ctx context.Context, transaction dbexec.Executor, query string, args ...any) ([]string, error) {
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
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("payloadrelease/close ids: %w", err)
	}
	return result, nil
}

// CollectScopeIDs gives terminal transition owners the same disciplined rows
// lifecycle used by the release worker without duplicating SQL iteration.

func CollectScopeIDs(ctx context.Context, executor dbexec.Executor, query string, args ...any) ([]string, error) {
	return collectIDs(ctx, executor, query, args...)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
