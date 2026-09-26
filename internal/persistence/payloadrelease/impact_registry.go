package payloadrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobregistry"
)

func globalReferenceCount(ctx context.Context, transaction dbapi.Executor, blobID string) (int64, error) {
	edges, err := blobregistry.Load()
	if err != nil {
		return 0, fmt.Errorf("payloadrelease/impact registry: %w", err)
	}
	var count int64
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		query := `SELECT count(*) FROM "` + edge.Table + `" WHERE "` + edge.Column + `"=?`
		var edgeCount int64
		if err := dbapi.QueryRowContext(ctx, transaction, query, blobID).Scan(&edgeCount); err != nil {
			return 0, fmt.Errorf("payloadrelease/impact global refs: %w", err)
		}
		count += edgeCount
	}
	return count, nil
}
