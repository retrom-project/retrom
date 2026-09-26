package firmware

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/firmware"
)

type (
	Repository          struct{ database dbapi.DB }
	requirementRecords  struct{ executor dbapi.Executor }
	uploadRecords       struct{ executor dbapi.Executor }
	installationRecords struct{ executor dbapi.Executor }
	archiveRecords      struct{ executor dbapi.Executor }
	writes              struct{ transaction dbapi.Tx }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func readScope(executor dbapi.Executor) firmware.ReadScope {
	return firmware.ReadScope{
		Requirements: requirementRecords{executor}, Uploads: uploadRecords{executor},
		Installations: installationRecords{executor}, Archives: archiveRecords{executor},
	}
}

func (repository *Repository) WithRead(ctx context.Context, work func(firmware.ReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin BIOS snapshot: %w", err)
	}
	defer dbapi.Rollback(transaction)
	if err := work(readScope(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit BIOS snapshot: %w", err)
	}
	return nil
}

func (repository *Repository) WithWrite(ctx context.Context, work func(firmware.WriteScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin BIOS write: %w", err)
	}
	defer dbapi.Rollback(transaction)
	bound := writes{transaction: transaction}
	if err := work(firmware.WriteScope{
		ReadScope: readScope(transaction), Archives: bound, Installations: bound,
		Retirements: BindSupersession(transaction), Server: bound, Blobs: bound,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit BIOS write: %w", err)
	}
	return nil
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
