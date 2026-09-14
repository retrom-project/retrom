package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/libraryimport"
	biopersistence "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/dbexec"
	metadatapersistence "retrom/internal/repo/metadatascrape"
	payloadpersistence "retrom/internal/repo/payloadrelease"
	tagpersistence "retrom/internal/repo/tagging"
)

type (
	ImportCreations struct{ database *sql.DB }
	creationRecords struct{ transaction *sql.Tx }
)

func NewImportCreations(database *sql.DB) *ImportCreations {
	return &ImportCreations{database: database}
}

func (repository *ImportCreations) WithCreation(
	ctx context.Context,
	work func(application.ImportCreationScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin import creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(BindImportCreation(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import creation: %w", err)
	}
	return nil
}

func BindImportCreation(tx *sql.Tx) application.ImportCreationScope {
	records := creationRecords{transaction: tx}
	return application.ImportCreationScope{
		Facts:      BindImportFacts(tx),
		Headers:    records,
		Sources:    records,
		Reviews:    records,
		Finish:     records,
		BIOS:       biopersistence.New(tx),
		Arcade:     creationArcadeRecords{executor: tx},
		Duplicates: BindContentDuplicates(tx),
		Claims:     BindReviewApproval(tx).Decisions,
		Tags:       tagpersistence.Bind(tx),
		Metadata:   metadatapersistence.BindSchedule(tx),
		Payload:    payloadpersistence.BindScheduling(tx),
		Ownership:  BindSourceOwnership(tx),
		Results:    BindSourceResults(tx),
	}
}

func creationMutation(result sql.Result, err error, action string, count int64) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s count: %w", action, err)
	}
	if changed != count {
		return fmt.Errorf("%s: %w", action, application.ErrVersionConflict)
	}
	return nil
}

func creationNullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
