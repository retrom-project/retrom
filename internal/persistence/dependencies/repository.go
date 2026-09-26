package dependencies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/datindex"
	service "retrom/internal/service/dependencies"
)

type (
	Repository     struct{ database dbapi.DB }
	targetRecords  struct{ executor dbapi.Executor }
	biosRecords    struct{ executor dbapi.Executor }
	datRecords     struct{ executor dbapi.Executor }
	catalogRecords struct{ transaction dbapi.Tx }
	jobRecords     struct{ executor dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) WithWrite(ctx context.Context, work func(service.WriteScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dependencies/begin: %w", err)
	}
	defer dbapi.Rollback(tx)
	scope := service.WriteScope{
		Targets: targetRecords{executor: tx}, BIOS: biosRecords{executor: tx},
		DAT: datRecords{
			executor: tx,
		}, Catalog: catalogRecords{
			transaction: tx,
		}, Jobs: jobRecords{
			executor: tx,
		}, Requirements: datindex.Bind(
			tx,
		),
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dependencies/commit: %w", err)
	}
	return nil
}

func (repository *Repository) TargetExists(ctx context.Context, target service.RuntimeTarget) (bool, error) {
	return targetRecords{executor: repository.database}.Exists(ctx, target)
}

func (records targetRecords) Exists(ctx context.Context, target service.RuntimeTarget) (bool, error) {
	var found int
	err := dbapi.QueryRowContext(
		ctx, records.executor,

		`SELECT 1 FROM runtime_targets WHERE provider_id=? AND target_id=?`,
		target.ProviderID,
		target.TargetID,
	).Scan(
		&found,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dependencies/read runtime target: %w", err)
	}
	return true, nil
}

func (repository *Repository) FindDAT(ctx context.Context, lookup service.DATLookup) (service.DATState, error) {
	var state service.DATState
	err := dbapi.QueryRowContext(ctx, repository.database, `
SELECT d.id,
d.parse_status,
(SELECT count(*)
FROM dat_machines m
WHERE m.dat_version_id=d.id)
FROM dat_versions d
WHERE d.sha256=?
AND d.parser_version='retrom-dat-v1'
AND d.core_id=?
AND d.provider_id=?
AND d.target_id=?
`, lookup.SHA256, lookup.CoreID, lookup.Target.ProviderID, lookup.Target.TargetID).
		Scan(&state.ID, &state.ParseStatus, &state.Indexed)
	if err != nil {
		return service.DATState{}, fmt.Errorf("dependencies/find DAT index: %w", err)
	}
	return state, nil
}
