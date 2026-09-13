package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type ItemWork struct{ database *sql.DB }

func NewItemWork(database *sql.DB) *ItemWork { return &ItemWork{database: database} }
func (repository *ItemWork) WithItemWork(ctx context.Context, run func(application.ItemWorkScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation item work: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := itemWorkRecords{transaction: tx, executor: tx}
	if err := run(application.ItemWorkScope{
		Payload: payload.BindReleases(tx), Read: records, Write: records,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation item work: %w", err)
	}
	return nil
}

type itemWorkRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records itemWorkRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return executionRecords(records).Current(ctx, id)
}

func (records itemWorkRecords) fence(ctx context.Context, before application.OwnedItem, now int64) error {
	return executionRecords(records).Fence(ctx, before.Execution, now)
}

const ownedItemPredicate = `id=? AND import_id=? AND version=? AND execution_state=?
AND metadata_json=? AND content_kind=? AND library_import_job_id IS ? AND library_import_item_id IS ?`

func ownedItemArguments(before application.OwnedItem) []any {
	item := before.Item
	return []any{
		item.ID,
		item.ImportID,
		item.Version,
		item.State,
		item.MetadataJSON,
		item.ContentKind,
		optionalText(item.LibraryImportJobID),
		optionalText(item.LibraryImportItemID),
	}
}
