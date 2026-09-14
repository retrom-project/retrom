package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/gamecontent"
	validation "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/dbexec"
)

type Repository struct {
	database *sql.DB
}
type (
	records struct{ executor dbexec.Executor }
	writes  struct {
		transaction *sql.Tx
	}
)

func New(database *sql.DB) *Repository {
	return &Repository{database: database}
}

func readScope(executor dbexec.Executor) gamecontent.ReadScope {
	bound := records{executor}
	return gamecontent.ReadScope{
		Content: bound,
		Inputs:  bound,
		BIOS:    validation.New(executor),
		Admin:   bound,
	}
}

func (repository *Repository) WithRead(ctx context.Context, work func(gamecontent.ReadScope) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin content replacement read: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(readScope(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit content replacement read: %w", err)
	}
	return nil
}

func (repository *Repository) WithWrite(ctx context.Context, work func(gamecontent.WriteScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin content replacement write: %w", err)
	}
	defer dbexec.Rollback(tx)
	bound := writes{tx}
	if err := work(
		gamecontent.WriteScope{
			ReadScope: readScope(
				tx,
			),
			Replays:            bound,
			Jobs:               bound,
			Leases:             bound,
			ContentWriter:      bound,
			Retirements:        BindRetirement(tx),
			AdminWriter:        bound,
			GameDeletionReader: bound,
			GameDeletionWriter: bound,
		},
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit content replacement write: %w", err)
	}
	return nil
}

func changed(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("write replacement record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count replacement writes: %w", err)
	}
	return count == 1, nil
}

func requireChanged(result sql.Result, err error) error {
	ok, err := changed(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return gamecontent.ErrExecutionLost
	}
	return nil
}
