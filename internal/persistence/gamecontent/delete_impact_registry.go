package gamecontent

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
)

func globalReferenceCount(ctx context.Context, transaction dbapi.Executor, blobID string) (int64, error) {
	var count int64
	if err := dbapi.QueryRowContext(
		ctx, transaction, `SELECT ref_count FROM blobs WHERE id=?`, blobID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("gamecontent/delete impact global refs: %w", err)
	}
	return count, nil
}
