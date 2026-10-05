package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

type (
	AdministrationRepository struct{ database dbapi.DB }
	administrationRecords    struct{ accountOperations }
)

func NewAdministration(database dbapi.DB) *AdministrationRepository {
	return &AdministrationRepository{database}
}

func (repository *AdministrationRepository) WithWrite(
	ctx context.Context,
	work func(accounts.AdministrationScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := administrationRecords{accountOperations{tx}}
		if err := work(accounts.AdministrationScope{Read: records, Write: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit account administration: %w", err)
	}
	return nil
}

func (records administrationRecords) Current(
	ctx context.Context,
	id string,
	now int64,
) (accounts.ManagedUser, bool, error) {
	user, err := scanAdminUser(dbapi.QueryRowContext(
		ctx, records.executor, adminUserProjection+` WHERE u.id=?`, now, now, id))
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.ManagedUser{}, false, nil
	}
	if err != nil {
		return accounts.ManagedUser{}, false, err
	}
	result := accounts.ManagedUser{User: user}
	if err := dbapi.QueryRowContext(
		ctx, records.executor,

		`SELECT profile_id FROM users WHERE id=?`,
		id,
	).Scan(
		&result.ProfileID,
	); err != nil {
		return result, false, fmt.Errorf("read managed profile: %w", err)
	}
	return result, true, nil
}
