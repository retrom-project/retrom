package database

import (
	"context"
	"database/sql"
	"fmt"
)

// QueryStrings reads one nullable string column and closes the result set.
func QueryStrings(ctx context.Context, queryer Queryer, query string, args ...any) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query strings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []string
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan string: %w", err)
		}
		if value.Valid {
			result = append(result, value.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate strings: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close strings: %w", err)
	}
	return result, nil
}
