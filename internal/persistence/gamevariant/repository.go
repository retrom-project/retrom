package gamevariant

import (
	"context"
	"fmt"
	"reflect"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamevariant"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func (r *Repository) Snapshot(ctx context.Context, gameID, coreID string) (application.Snapshot, error) {
	tx, err := r.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.Snapshot{}, fmt.Errorf("begin variant snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
	result, err := ReadSnapshot(ctx, tx, gameID, coreID)
	if err != nil {
		return application.Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.Snapshot{}, fmt.Errorf("commit variant snapshot: %w", err)
	}
	return result, nil
}

func (r *Repository) WithEnsure(ctx context.Context, work func(application.EnsureScope) error) error {
	err := dbapi.RetryTransaction(ctx, r.database, func(tx dbapi.Tx) error {
		scope := application.EnsureScope{
			Read: func(ctx context.Context, gameID, coreID string) (application.Snapshot, error) {
				return ReadSnapshot(ctx, tx, gameID, coreID)
			},
			Write: NewWriteScope(tx),
		}
		if err := work(scope); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit variant ensure: %w", err)
	}
	return nil
}

func ReadSnapshot(ctx context.Context, executor dbapi.Executor, gameID, coreID string) (application.Snapshot, error) {
	source, found, err := Source(ctx, executor, gameID, coreID)
	if err != nil || !found {
		return application.Snapshot{}, err
	}
	snapshot, err := ContentSnapshot(ctx, executor, source)
	if err != nil {
		return application.Snapshot{}, err
	}
	SourceNames(&snapshot.Source, snapshot.GameFiles)
	snapshot.BIOS, err = BIOSFacts(ctx, executor, snapshot.Source, source.DATVersionID)
	if err != nil {
		return application.Snapshot{}, err
	}
	snapshot.ValidationBIOS = snapshot.BIOS
	if !reflect.DeepEqual(source.DATVersionID, source.ActiveDATVersionID) {
		snapshot.ValidationBIOS, err = BIOSFacts(ctx, executor, snapshot.Source, source.ActiveDATVersionID)
		if err != nil {
			return application.Snapshot{}, err
		}
	}
	return snapshot, nil
}
