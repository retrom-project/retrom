package accounts

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

type (
	Authentication struct{ reader, writer dbapi.DB }
	authRecords    struct{ executor dbapi.Executor }
)

func NewAuthentication(reader, writer dbapi.DB) *Authentication {
	return &Authentication{reader: reader, writer: writer}
}

func (repository *Authentication) Credential(
	ctx context.Context,
	username string,
) (accounts.LoginCredential, bool, error) {
	return (authRecords{repository.reader}).Credential(ctx, username)
}

func (repository *Authentication) Session(ctx context.Context, hash [32]byte) (accounts.SessionSnapshot, bool, error) {
	return (authRecords{repository.reader}).Session(ctx, hash)
}

func (repository *Authentication) WithWrite(ctx context.Context, work func(accounts.AuthScope) error) error {
	tx, err := repository.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin authentication: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := authRecords{tx}
	if err := work(accounts.AuthScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit authentication: %w", err)
	}
	return nil
}
