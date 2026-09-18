package launch

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

type Config struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewConfig(database *sql.DB) *Config { return &Config{database: database} }

func (repository *Config) WithPreCommitHook(hook func() error) {
	repository.preCommitHook = hook
}

func (repository *Config) Load(
	ctx context.Context,
	ref application.SessionRef,
	authorize application.ConfigAuthorization,
) (application.ConfigSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ConfigSnapshot{}, false, fmt.Errorf("begin config snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	source, found, err := configSource(ctx, tx, ref)
	if err != nil || !found {
		return application.ConfigSnapshot{}, false, err
	}
	if err := authorize(source); err != nil {
		return application.ConfigSnapshot{}, false, err
	}
	authority, err := configAuthority(ctx, tx, ref, source)
	if err != nil {
		return application.ConfigSnapshot{}, false, err
	}

	files, err := configFiles(ctx, tx, ref, false)
	if err != nil {
		return application.ConfigSnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ConfigSnapshot{}, false, fmt.Errorf("commit config snapshot: %w", err)
	}
	return application.ConfigSnapshot{Authority: authority, Files: files}, true, nil
}

func (repository *Config) LoadAuthority(
	ctx context.Context,
	ref application.SessionRef,
) (application.ConfigAuthority, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ConfigAuthority{}, false,
			fmt.Errorf("begin config authority read: %w", err)
	}
	defer dbexec.Rollback(tx)
	authority, found, err := (configRecords{executor: tx}).Current(ctx, ref)
	if err != nil {
		return application.ConfigAuthority{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ConfigAuthority{}, false,
			fmt.Errorf("commit config authority read: %w", err)
	}
	return authority, found, nil
}

func (repository *Config) CommitActivation(ctx context.Context, plan application.ConfigActivationPlan) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin config activation: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := (configRecords{executor: tx}).Activate(ctx, plan); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config activation: %w", err)
	}
	return nil
}

type configRecords struct{ executor dbexec.Executor }

func (records configRecords) Current(
	ctx context.Context,
	ref application.SessionRef,
) (application.ConfigAuthority, bool, error) {
	source, found, err := configSource(ctx, records.executor, ref)
	if err != nil || !found {
		return application.ConfigAuthority{}, false, err
	}
	authority, err := configAuthority(ctx, records.executor, ref, source)
	return authority, err == nil, err
}

func configAuthority(
	ctx context.Context,
	executor dbexec.Executor,
	ref application.SessionRef,
	source application.ConfigSource,
) (application.ConfigAuthority, error) {
	restore, err := configRestore(ctx, executor, ref, source)
	if err != nil {
		return application.ConfigAuthority{}, err
	}
	var grants []application.IsolationGrant
	if source.Delivery == "ISOLATED_WEB_PROJECT" {
		grants, err = configIsolation(ctx, executor, ref)
		if err != nil {
			return application.ConfigAuthority{}, err
		}
	}
	return application.ConfigAuthority{Source: source, Restore: restore, Isolation: grants}, nil
}

func (repository *Config) Project(
	ctx context.Context,
	id string,
	authorize application.ConfigAuthorization,
) (application.ConfigSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ConfigSnapshot{}, false, fmt.Errorf("begin project identity: %w", err)
	}
	defer dbexec.Rollback(tx)
	ref := application.SessionRef{ID: id}
	source, found, err := configSource(ctx, tx, ref)
	if err != nil {
		return application.ConfigSnapshot{}, false, err
	}
	if !found {
		ref.Preview = true
		source, found, err = configSource(ctx, tx, ref)
	}
	if err != nil || !found {
		return application.ConfigSnapshot{}, false, err
	}
	if err := authorize(source); err != nil {
		return application.ConfigSnapshot{}, false, err
	}
	files, err := configFiles(ctx, tx, ref, true)
	if err != nil {
		return application.ConfigSnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ConfigSnapshot{}, false, fmt.Errorf("commit project identity: %w", err)
	}
	return application.ConfigSnapshot{Authority: application.ConfigAuthority{Source: source}, Files: files}, true, nil
}
