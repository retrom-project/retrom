package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	payload "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/libraryimport"
)

type (
	ImportExecutions       struct{ database *sql.DB }
	importExecutionRecords struct{ executor dbexec.Executor }
)

func NewImportExecutions(database *sql.DB) *ImportExecutions {
	return &ImportExecutions{database: database}
}

func (repository *ImportExecutions) WithExecution(
	ctx context.Context,
	work func(application.ImportExecutionScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin import execution: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := application.ImportExecutionScope{
		Records: importExecutionRecords{executor: tx},
		Facts:   BindImportFacts(tx),
		Payload: payload.BindScheduling(tx),
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import execution: %w", err)
	}
	return nil
}

func (repository *ImportExecutions) Queued(ctx context.Context, now int64) ([]string, error) {
	return repository.ids(
		ctx,
		`SELECT job.id FROM jobs job JOIN import_jobs parent ON parent.id=job.scope_id
WHERE job.kind='IMPORT_GROUP' AND job.scope_type='IMPORT_GROUP' AND job.state='QUEUED' AND
 job.available_at_ms<=?
AND parent.state IN ('QUEUED','FAILED') ORDER BY job.available_at_ms,job.id LIMIT 64`,
		now,
	)
}

func (repository *ImportExecutions) Recoverable(ctx context.Context, now int64) ([]string, error) {
	return repository.ids(
		ctx,
		`SELECT job.id FROM jobs job JOIN import_jobs parent ON parent.id=job.scope_id
WHERE job.kind='IMPORT_GROUP' AND job.scope_type='IMPORT_GROUP' AND (
(job.state IN ('RUNNING','CANCEL_REQUESTED') AND (COALESCE(job.leased_until_ms,0)<=? OR
 job.execution_deadline_at_ms<=?))
 OR (job.state='QUEUED' AND (job.execution_deadline_at_ms<=? OR job.attempt_count>=job.max_attempts
 OR EXISTS(SELECT 1 FROM import_items WHERE import_job_id=parent.id)
 OR EXISTS(SELECT 1 FROM import_job_files WHERE import_job_id=parent.id AND disposition!='PENDING')))
 OR (job.state='CANCELLED' AND parent.state!='CANCELLED')) ORDER BY job.id LIMIT 64`,
		now,
		now,
		now,
	)
}

func (repository *ImportExecutions) ids(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := repository.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query import execution queue: %w", err)
	}
	defer func() { cleanup.Error("close import execution queue", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan import execution queue: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import execution queue: %w", err)
	}
	return result, nil
}
