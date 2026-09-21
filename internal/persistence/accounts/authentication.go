package accounts

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/service/accounts"
)

type (
	Authentication struct{ database *sql.DB }
	authRecords    struct{ executor dbexec.Executor }
)

func NewAuthentication(database *sql.DB) *Authentication { return &Authentication{database} }
func (repository *Authentication) Credential(
	ctx context.Context,
	username string,
) (accounts.LoginCredential, bool, error) {
	return (authRecords{repository.database}).Credential(ctx, username)
}

func (repository *Authentication) Session(ctx context.Context, hash [32]byte) (accounts.SessionSnapshot, bool, error) {
	return (authRecords{repository.database}).Session(ctx, hash)
}

func (repository *Authentication) WithWrite(ctx context.Context, work func(accounts.AuthScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin authentication: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := authRecords{tx}
	if err := work(accounts.AuthScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authentication: %w", err)
	}
	return nil
}
