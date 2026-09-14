package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
)

type (
	AdministrationRepository struct{ database *sql.DB }
	administrationRecords    struct{ accountOperations }
)

func NewAdministration(database *sql.DB) *AdministrationRepository {
	return &AdministrationRepository{database}
}

func (repository *AdministrationRepository) WithWrite(
	ctx context.Context,
	work func(accounts.AdministrationScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account administration: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := administrationRecords{accountOperations{tx}}
	if err := work(accounts.AdministrationScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account administration: %w", err)
	}
	return nil
}

func (records administrationRecords) Current(
	ctx context.Context,
	id string,
	now int64,
) (accounts.ManagedUser, bool, error) {
	user, err := scanAdminUser(records.executor.QueryRowContext(ctx, adminUserProjection+` WHERE u.id=?`, now, now, id))
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.ManagedUser{}, false, nil
	}
	if err != nil {
		return accounts.ManagedUser{}, false, err
	}
	result := accounts.ManagedUser{User: user}
	if err := records.executor.QueryRowContext(
		ctx,
		`SELECT profile_id FROM users WHERE id=?`,
		id,
	).Scan(
		&result.ProfileID,
	); err != nil {
		return result, false, fmt.Errorf("read managed profile: %w", err)
	}
	return result, true, nil
}

func (records administrationRecords) AnotherEnabledAdmin(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id!=? AND role='ADMIN' AND status='ENABLED')`,
		id,
	).Scan(
		&exists,
	)
	if err != nil {
		return false, fmt.Errorf("query remaining administrator: %w", err)
	}
	return exists, nil
}
