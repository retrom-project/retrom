package gamecontent

import (
	"context"

	dbapi "retrom/internal/database"
	gamerefs "retrom/internal/persistence/gamecontent/references"
)

func gameImpactBlobIDs(ctx context.Context, transaction dbapi.Executor, gameID string) ([]string, error) {
	ids, err := gamerefs.GameBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return nil, wrapErr(err)
	}
	return uniqueImpactStrings(ids), nil
}

func uniqueImpactStrings(values []string) []string {
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
