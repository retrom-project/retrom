package accounts

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
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

func (repository *Authentication) CommitLogin(
	ctx context.Context, cmd accounts.LoginCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin login: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := authRecords{tx}
	if err := records.Login(ctx, cmd.Credential, cmd.Session); err != nil {
		return fmt.Errorf("create login session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit login: %w", err)
	}
	return nil
}

func (repository *Authentication) CommitLogout(
	ctx context.Context, cmd accounts.LogoutCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin logout: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := authRecords{tx}
	if err := records.Revoke(ctx, cmd.SessionID, cmd.NowMS); err != nil {
		return fmt.Errorf("revoke authentication session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit logout: %w", err)
	}
	return nil
}

func (repository *Authentication) CommitRefreshSession(
	ctx context.Context, cmd accounts.RefreshSessionCommand,
) (accounts.SessionSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return accounts.SessionSnapshot{}, fmt.Errorf("begin session refresh: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := authRecords{tx}
	current, found, err := records.Session(ctx, cmd.Digest)
	if err != nil {
		return accounts.SessionSnapshot{}, fmt.Errorf("recheck authentication session: %w", err)
	}
	if !found || !accounts.ValidSession(current, cmd.NowMS) {
		return accounts.SessionSnapshot{}, accounts.ErrAuthenticationNeeded
	}
	if cmd.NowMS-current.LastSeen >= accounts.RefreshInterval.Milliseconds() {
		expiry := min(cmd.NowMS+accounts.IdleDuration.Milliseconds(), current.AbsoluteExpiry)
		if err := records.Refresh(ctx, accounts.SessionRefresh{
			ID:               current.ID,
			ExpectedLastSeen: current.LastSeen,
			LastSeen:         cmd.NowMS,
			IdleExpiry:       expiry,
		}); err != nil {
			return accounts.SessionSnapshot{}, fmt.Errorf("refresh authentication session: %w", err)
		}
		current.LastSeen = cmd.NowMS
		current.IdleExpiry = expiry
	}
	if err := tx.Commit(); err != nil {
		return accounts.SessionSnapshot{}, fmt.Errorf("commit session refresh: %w", err)
	}
	return current, nil
}
