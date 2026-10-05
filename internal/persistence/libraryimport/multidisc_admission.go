package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type (
	MultiDiscAttachments      struct{ database dbapi.DB }
	multidiscAdmissionRecords struct{ executor dbapi.Executor }
)

func NewMultiDiscAttachments(database dbapi.DB) *MultiDiscAttachments {
	return &MultiDiscAttachments{database}
}

func BindMultiDiscAdmission(executor dbapi.Executor) libraryservice.MultiDiscAttachmentReader {
	return multidiscAdmissionRecords{executor}
}

func (repository *MultiDiscAttachments) WithAttachmentAdmission(
	ctx context.Context,
	run func(libraryservice.MultiDiscAttachmentScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := multidiscAdmissionRecords{tx}
		if err := run(libraryservice.MultiDiscAttachmentScope{Read: records, Queue: records, Review: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
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
		return &libraryservice.MultiDiscAttachmentError{Code: code, Cause: libraryservice.ErrInvalid}
	}
	return nil
}
