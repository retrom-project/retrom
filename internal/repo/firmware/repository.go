package firmware

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/firmware"
	"retrom/internal/repo/dbexec"
)

type (
	Repository          struct{ database *sql.DB }
	requirementRecords  struct{ executor dbexec.Executor }
	uploadRecords       struct{ executor dbexec.Executor }
	installationRecords struct{ executor dbexec.Executor }
	archiveRecords      struct{ executor dbexec.Executor }
	writes              struct{ transaction *sql.Tx }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }
func readScope(executor dbexec.Executor) firmware.ReadScope {
	return firmware.ReadScope{
		Requirements: requirementRecords{executor}, Uploads: uploadRecords{executor},
		Installations: installationRecords{executor}, Archives: archiveRecords{executor},
	}
}

func (repository *Repository) WithRead(ctx context.Context, work func(firmware.ReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin BIOS snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(readScope(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit BIOS snapshot: %w", err)
	}
	return nil
}

func (repository *Repository) writeScope(tx *sql.Tx) firmware.WriteScope {
	bound := writes{transaction: tx}
	return firmware.WriteScope{
		ReadScope: readScope(tx), Archives: bound, Installations: bound,
		Retirements: BindSupersession(tx), Server: bound, Blobs: bound,
	}
}

func (repository *Repository) CommitBrowserInstall(
	ctx context.Context,
	cmd firmware.BrowserInstallCommand,
) (firmware.Installation, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return firmware.Installation{}, fmt.Errorf("begin BIOS write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	result, err := browserInstall(ctx, scope, cmd)
	if err != nil {
		return firmware.Installation{}, err
	}
	if err := tx.Commit(); err != nil {
		return firmware.Installation{}, fmt.Errorf("commit BIOS write: %w", err)
	}
	return result, nil
}

func (repository *Repository) CommitServerInstall(
	ctx context.Context,
	cmd firmware.ServerInstallCommand,
) (firmware.ServerInstallResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return firmware.ServerInstallResult{}, fmt.Errorf("begin BIOS write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	result, err := serverInstall(ctx, scope, cmd)
	if err != nil {
		return firmware.ServerInstallResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return firmware.ServerInstallResult{}, fmt.Errorf("commit BIOS write: %w", err)
	}
	return result, nil
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write BIOS record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count BIOS write: %w", err)
	}
	if count != 1 {
		return firmware.ErrInvalid
	}
	return nil
}
