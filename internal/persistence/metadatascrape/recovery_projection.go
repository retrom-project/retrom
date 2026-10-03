package metadatascrape

import (
	"fmt"

	dbapi "retrom/internal/database"
)

func readRecoveryIDs(rows dbapi.Rows) ([]string, error) {
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recovery job identity: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery queue: %w", err)
	}
	return ids, nil
}
