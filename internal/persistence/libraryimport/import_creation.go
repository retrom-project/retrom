package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	biopersistence "retrom/internal/persistence/corevalidation"
	payloadpersistence "retrom/internal/persistence/libraryimport/itemrelease"
	metadatapersistence "retrom/internal/persistence/metadatascrape"
	tagpersistence "retrom/internal/persistence/tagging"
	libraryservice "retrom/internal/service/libraryimport"
)

type (
	ImportCreations struct{ database dbapi.DB }
	creationRecords struct{ transaction dbapi.Tx }
)

func NewImportCreations(database dbapi.DB) *ImportCreations {
	return &ImportCreations{database: database}
}

func (repository *ImportCreations) WithCreation(
	ctx context.Context,
	work func(libraryservice.ImportCreationScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{Isolation: dbapi.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin import creation: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := work(BindImportCreation(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import creation: %w", err)
	}
	return nil
}

func BindImportCreation(tx dbapi.Tx) libraryservice.ImportCreationScope {
	records := creationRecords{transaction: tx}
	return libraryservice.ImportCreationScope{
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
		return fmt.Errorf("%s: %w", action, libraryservice.ErrVersionConflict)
	}
	return nil
}

func creationNullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
