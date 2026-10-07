package persistence

import (
	"context"
	"encoding/json"
	"fmt"
)

func readJSON[T any](ctx context.Context, r *Repository, query string, args ...any) (T, error) {
	var value T
	var data []byte
	if err := r.db.QueryRow(ctx, query, args...).Scan(&data); err != nil {
		return value, failure("read record", err)
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode record: %w", err)
	}
	return value, nil
}

func listJSON[T any](ctx context.Context, r *Repository, query string, args ...any) ([]T, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, failure("query records", err)
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		var data []byte
		var value T
		if err = rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("read record: %w", err)
		}
		if err = json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("decode record: %w", err)
		}
		result = append(result, value)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate records: %w", err)
	}
	return result, nil
}

func (r *Repository) Count(ctx context.Context, query string, args ...any) (int64, error) {
	var count int64
	if err := r.db.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, failure("count records", err)
	}
	return count, nil
}

func (r *Repository) Execute(ctx context.Context, query string, args ...any) error {
	if _, err := r.db.Exec(ctx, query, args...); err != nil {
		return failure("write records", err)
	}
	return nil
}
