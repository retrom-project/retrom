package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/payloadrelease"
	validation "retrom/internal/persistence/corevalidation"
	"retrom/internal/service/gamecontent"
)

type Repository struct {
	database *sql.DB
	releases *payloadrelease.Service
}
type (
	records struct{ executor dbexec.Executor }
	writes  struct {
		transaction *sql.Tx
		releases    *payloadrelease.Service
	}
)

func New(database *sql.DB, releases *payloadrelease.Service) *Repository {
	return &Repository{database, releases}
}

func readScope(executor dbexec.Executor) gamecontent.ReadScope {
	return gamecontent.ReadScope{Content: records{executor}, Inputs: records{executor}, BIOS: validation.New(executor)}
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
	bound := writes{tx, repository.releases}
	if err := work(
		gamecontent.WriteScope{
			ReadScope: readScope(
				tx,
			),
			Replays:       bound,
			Jobs:          bound,
			Leases:        bound,
			ContentWriter: bound,
			Retirements:   bound,
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
