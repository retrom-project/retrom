package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	metadatapersistence "retrom/internal/persistence/metadatascrape"
	tagpersistence "retrom/internal/persistence/tagging"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewDetail struct{ database dbapi.DB }

func NewReviewDetail(database dbapi.DB) *ReviewDetail { return &ReviewDetail{database: database} }
func (repository *ReviewDetail) WithRead(ctx context.Context, work func(libraryservice.ReviewReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin review detail snapshot: %w", err)
	}
	defer dbapi.Rollback(transaction)
	scope := libraryservice.ReviewReadScope{
		Drafts: ReviewDrafts{transaction}, Media: ReviewMedia{transaction}, Sources: ReviewSources{transaction},
		Profiles:     BindReviewInputs(transaction),
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
