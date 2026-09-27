package payloadrelease

import (
	"context"

	dbapi "retrom/internal/database"
)

func collectIDs(ctx context.Context, transaction dbapi.Executor, query string, args ...any) ([]string, error) {
	return dbapi.QueryStrings(ctx, transaction, query, args...)
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
