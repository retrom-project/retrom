package libraryimport

import (
	"database/sql"
	"fmt"

	libraryservice "retrom/internal/service/libraryimport"
)

func collectRows[T any](
	rows *sql.Rows,
	scan func(*sql.Rows) (T, error),
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

func scanQueuedJob(rows *sql.Rows) (libraryservice.QueuedJob, error) {
	var job libraryservice.QueuedJob
	if err := rows.Scan(&job.ID, &job.AvailableAtMS); err != nil {
		return libraryservice.QueuedJob{}, fmt.Errorf("scan queued job row: %w", err)
	}
	return job, nil
}
