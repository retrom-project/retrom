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
	// Password verification and session minting precede this database-only scope.
	err := dbapi.RetryTransaction(ctx, repository.writer, func(tx dbapi.Tx) error {
		records := authRecords{tx}
		return work(accounts.AuthScope{Read: records, Write: records, Limits: rateLimitRecords{tx}})
	})
	if err != nil {
		return fmt.Errorf("commit authentication: %w", err)
	}
	return nil
}
