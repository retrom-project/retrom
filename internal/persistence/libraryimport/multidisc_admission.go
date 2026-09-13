package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

type (
	MultiDiscAttachments      struct{ database *sql.DB }
	multidiscAdmissionRecords struct{ executor dbexec.Executor }
)

func NewMultiDiscAttachments(database *sql.DB) *MultiDiscAttachments {
	return &MultiDiscAttachments{database}
}

func BindMultiDiscAdmission(executor dbexec.Executor) application.MultiDiscAttachmentReader {
	return multidiscAdmissionRecords{executor}
}

func (repository *MultiDiscAttachments) WithAttachmentAdmission(
	ctx context.Context,
	run func(application.MultiDiscAttachmentScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin multi-disc attachment admission: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := multidiscAdmissionRecords{tx}
	if err := run(application.MultiDiscAttachmentScope{Read: records, Queue: records, Review: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit multi-disc attachment admission: %w", err)
	}
	return nil
}

func attachmentAdmissionCount(result sql.Result, err error, code string) error {
	if err != nil {
		return fmt.Errorf("write multi-disc attachment admission: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count multi-disc attachment admission: %w", err)
	}
	if count != 1 {
		return &application.MultiDiscAttachmentError{Code: code, Cause: application.ErrInvalid}
	}
	return nil
}
