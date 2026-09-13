package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	metadatapersistence "retrom/internal/persistence/metadatascrape"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
)

type ReviewDetail struct{ database *sql.DB }

func NewReviewDetail(database *sql.DB) *ReviewDetail { return &ReviewDetail{database: database} }
func (repository *ReviewDetail) WithRead(ctx context.Context, work func(application.ReviewReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin review detail snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)
	scope := application.ReviewReadScope{
		Drafts: ReviewDrafts{transaction}, Media: ReviewMedia{transaction}, Sources: ReviewSources{transaction},
		Validation:   BindReviewValidation(transaction),
		Duplicates:   BindContentDuplicates(transaction),
		Dependencies: BindReviewDependencies(transaction),

		Metadata: metadatapersistence.BindEvidenceQueries(transaction), Tags: tagpersistence.Bind(transaction).Relations,
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("finish review detail snapshot: %w", err)
	}
	return nil
}
