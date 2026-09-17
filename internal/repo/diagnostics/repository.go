package diagnostics

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/diagnostics"
	"retrom/internal/repo/dbexec"
)

type Repository struct {
	database *sql.DB
}

type records struct {
	executor dbexec.Executor
}

func New(database *sql.DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) LoadReport(ctx context.Context) (application.Report, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.Report{}, fmt.Errorf("diagnostics: begin read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	reader := records{executor: transaction}
	var report application.Report
	report.DatabaseSchemaVersion, err = reader.SchemaVersion(ctx)
	if err != nil {
		return application.Report{}, err
	}
	report.Counts, err = reader.Counts(ctx)
	if err != nil {
		return application.Report{}, err
	}
	report.RuntimeProviders, err = reader.RuntimeProviders(ctx)
	if err != nil {
		return application.Report{}, err
	}
	if err := transaction.Commit(); err != nil {
		return application.Report{}, fmt.Errorf("diagnostics: commit read: %w", err)
	}
	return report, nil
}
