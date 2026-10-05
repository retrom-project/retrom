package accounts

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

type (
	InitializationRepository struct{ reader, writer dbapi.DB }
	initializationRecords    struct{ executor dbapi.Executor }
)

func NewInitialization(reader, writer dbapi.DB) *InitializationRepository {
	return &InitializationRepository{reader: reader, writer: writer}
}

func (repository *InitializationRepository) State(ctx context.Context) (accounts.InitializationState, error) {
	return (initializationRecords{repository.reader}).State(ctx)
}

func (repository *InitializationRepository) WithWrite(
	ctx context.Context, work func(accounts.InitializationScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.writer, func(tx dbapi.Tx) error {
		records := initializationRecords{tx}
		return work(accounts.InitializationScope{Read: records, Write: records})
	})
	if err != nil {
		return fmt.Errorf("commit accounts transaction: %w", err)
	}
	return nil
}

func (records initializationRecords) State(ctx context.Context) (accounts.InitializationState, error) {
	var state accounts.InitializationState
	err := dbapi.QueryRowContext(
		ctx, records.executor,

		`SELECT state,test_default_password_active,(SELECT count(*) FROM users),(SELECT count(*) FROM profiles),
 (SELECT count(*) FROM users WHERE role='ADMIN' AND status='ENABLED'),
 (SELECT count(*) FROM profiles p LEFT JOIN users u ON u.profile_id=p.id WHERE u.id IS NULL)
 FROM instance_state WHERE id=1`,
	).
		Scan(&state.State, &state.TestDefault, &state.Users, &state.Profiles, &state.EnabledAdmins, &state.OrphanProfiles)
	if err != nil {
		return state, fmt.Errorf("query account initialization snapshot: %w", err)
	}
	return state, nil
}

func (repository *InitializationRepository) Credentials(ctx context.Context) ([]accounts.StoredCredential, error) {
	rows, err := repository.reader.QueryContext(
		ctx,
		`SELECT COALESCE(c.password_scheme,''),COALESCE(c.password_hash,''),c.user_id IS NULL
 FROM users u LEFT JOIN user_credentials c ON c.user_id=u.id
 WHERE u.status<>'DELETED' ORDER BY u.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query credential store: %w", err)
	}
	defer func() { cleanup.Error("close credential store", rows.Close()) }()
	credentials := make([]accounts.StoredCredential, 0)
	for rows.Next() {
		var value accounts.StoredCredential
		if err := rows.Scan(&value.Scheme, &value.Hash, &value.Missing); err != nil {
			return nil, fmt.Errorf("scan credential store: %w", err)
		}
		credentials = append(credentials, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credential store: %w", err)
	}
	return credentials, nil
}
