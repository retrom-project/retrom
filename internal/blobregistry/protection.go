package blobregistry

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ProtectiveSet returns the exact Blob set retained by the GC registry. A
// protected archive also protects its materialized members, but ownership is
// intentionally one-way.
func ProtectiveSet(ctx context.Context, database Queryer) (map[string]struct{}, error) {
	edges, err := Load()
	if err != nil {
		return nil, fmt.Errorf("blobregistry/protection: %w", err)
	}
	protected := map[string]struct{}{}
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		query := `
SELECT DISTINCT ` + quoteIdentifier(edge.Column) + `
FROM ` + quoteIdentifier(edge.Table) + `
WHERE ` + quoteIdentifier(edge.Column) + ` IS NOT NULL`
		if err := collectProtected(ctx, database, query, nil, protected); err != nil {
			return nil, err
		}
	}
	if len(protected) == 0 {
		return protected, nil
	}
	if err := collectMaterializedMembers(ctx, database, protected); err != nil {
		return nil, err
	}
	return protected, nil
}

func collectMaterializedMembers(
	ctx context.Context,
	database Queryer,
	protected map[string]struct{},
) error {
	rows, err := database.QueryContext(ctx, `
SELECT DISTINCT archive_blob_id, materialized_blob_id
FROM archive_entries
WHERE materialized_blob_id IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("blobregistry/protection: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var owner, member string
		if err := rows.Scan(&owner, &member); err != nil {
			return fmt.Errorf("blobregistry/protection: %w", err)
		}
		if _, ok := protected[owner]; ok {
			protected[member] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("blobregistry/protection: %w", err)
	}
	return nil
}

func collectProtected(
	ctx context.Context,
	database Queryer,
	query string,
	values []any,
	protected map[string]struct{},
) error {
	rows, err := database.QueryContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("blobregistry/protection: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("blobregistry/protection: %w", err)
		}
		protected[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("blobregistry/protection: %w", err)
	}
	return nil
}
