package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	library "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/emulationstationimport"
)

type ReviewHandoff struct{ database *sql.DB }

func NewReviewHandoff(database *sql.DB) *ReviewHandoff { return &ReviewHandoff{database: database} }
func (repository *ReviewHandoff) WithReviewHandoff(
	ctx context.Context,
	run func(application.ReviewHandoffScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation review handoff: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := reviewHandoffRecords{transaction: tx, executor: tx}
	scope := application.ReviewHandoffScope{
		Read: records, Write: executionRecords(records), Metadata: library.BindMetadata(tx),
	}
	if err := run(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation review handoff: %w", err)
	}
	return nil
}

type reviewHandoffRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records reviewHandoffRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return executionRecords(records).Current(ctx, id)
}

func (records reviewHandoffRecords) Review(
	ctx context.Context,
	importID, id string,
) (application.ExecutionReview, bool, error) {
	review, err := scanExecutionReview(records.executor.QueryRowContext(ctx, executionReviewSQL+`
WHERE source.id=? AND source.import_id=?
AND item.state='REVIEW_PENDING' AND item.review_handoff_kind='EMULATIONSTATION'`, id, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return application.ExecutionReview{}, false, nil
	}
	if err != nil {
		return application.ExecutionReview{}, false, fmt.Errorf("read EmulationStation review handoff: %w", err)
	}
	return review, true, nil
}
