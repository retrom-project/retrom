package gamecontent

import (
	"context"

	dbapi "retrom/internal/database"
)

func gameImpactBlobIDs(ctx context.Context, transaction dbapi.Executor, gameID string) ([]string, error) {
	ids, err := dbapi.QueryStrings(
		ctx,
		transaction,
		`SELECT id FROM stored_files WHERE (owner_kind='GAME' AND owner_id=?1)
 OR (owner_kind='SAVE_STATE' AND owner_id IN(SELECT id FROM save_states WHERE game_id=?1))
 OR (owner_kind='SCRAPE_RUN' AND owner_id IN(SELECT id FROM metadata_scrape_runs WHERE game_id=?1)) ORDER BY id`,
		gameID,
	)
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
