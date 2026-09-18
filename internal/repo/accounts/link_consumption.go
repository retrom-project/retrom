package accounts

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
)

func (repository *LinkRepository) LoadInvitationLink(
	ctx context.Context, id string,
) (accounts.LinkRecord, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return accounts.LinkRecord{}, false,
			fmt.Errorf("begin invitation link snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	link, found, err := records.Current(ctx, id)
	if err != nil {
		return accounts.LinkRecord{}, false,
			fmt.Errorf("read invitation link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return accounts.LinkRecord{}, false,
			fmt.Errorf("finish invitation link snapshot: %w", err)
	}
	return link, found, nil
}

func (repository *LinkRepository) CommitInvitationAcceptance(
	ctx context.Context, cmd accounts.InvitationAcceptCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	exists, err := records.UsernameExists(ctx, cmd.Plan.User.Username)
	if err != nil {
		return fmt.Errorf("check invited username: %w", err)
	}
	if exists {
		return accounts.ErrUsernameUnavailable
	}
	if err := records.Accept(ctx, cmd.Plan); err != nil {
		return fmt.Errorf("accept invitation: %w", err)
	}
	if err := records.Audit(ctx, cmd.Audit); err != nil {
		return fmt.Errorf("audit invitation acceptance: %w", err)
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return nil
}

func (repository *LinkRepository) CommitPasswordReset(
	ctx context.Context, cmd accounts.PasswordResetCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	if err := records.Reset(ctx, cmd.Plan); err != nil {
		return fmt.Errorf("consume password reset: %w", err)
	}
	if err := records.Audit(ctx, cmd.Audit); err != nil {
		return fmt.Errorf("audit password reset: %w", err)
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}
	return nil
}

func (repository *LinkRepository) ResetState(ctx context.Context, id string) (accounts.ResetState, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return accounts.ResetState{}, false, fmt.Errorf("begin reset capability snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	link, found, err := records.Current(ctx, id)
	if err != nil {
		return accounts.ResetState{}, false, err
	}
	if !found || link.Link.TargetUserID == nil {
		return accounts.ResetState{}, false, nil
	}
	target, found, err := records.Target(ctx, *link.Link.TargetUserID)
	if err != nil {
		return accounts.ResetState{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return accounts.ResetState{}, false, fmt.Errorf("finish reset capability snapshot: %w", err)
	}
	return accounts.ResetState{Link: link, Target: target}, found, nil
}

func (records linkRecords) UsernameExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username=?)`,
		username,
	).Scan(
		&exists,
	)
	if err != nil {
		return false, fmt.Errorf("query invited username: %w", err)
	}
	return exists, nil
}
