package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type Companions struct{ database *sql.DB }

func NewCompanions(database *sql.DB) *Companions { return &Companions{database: database} }
func (repository *Companions) WithCompanions(ctx context.Context, run func(application.CompanionScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation companions: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := companionRecords{transaction: tx, executor: tx}
	if err := run(application.CompanionScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation companions: %w", err)
	}
	return nil
}

type companionRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}
