package libraryimport

import (
	"fmt"

	dbapi "retrom/internal/database"
)

func collectRows[T any](
	rows dbapi.Rows,
	scan func(dbapi.Rows) (T, error),
	scanLabel, iterateLabel string,
) ([]T, error) {
	result := make([]T, 0)
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", scanLabel, err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", iterateLabel, err)
	}
	return result, nil
}
