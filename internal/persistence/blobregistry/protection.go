package blobregistry

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
)

// ProtectiveSet returns Blobs with a live owner count. The database updates
// counts transactionally when owners or materialized archive entries change.
func ProtectiveSet(ctx context.Context, database dbapi.Queryer) (map[string]struct{}, error) {
	rows, err := database.QueryContext(ctx, `SELECT id FROM blobs WHERE ref_count>0`)
	if err != nil {
		return nil, fmt.Errorf("blobregistry/protection: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	protected := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("blobregistry/protection: %w", err)
		}
		protected[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("blobregistry/protection: %w", err)
	}
	return protected, nil
}
