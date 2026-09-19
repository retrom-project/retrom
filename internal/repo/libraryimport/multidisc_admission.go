package libraryimport

import (
	"database/sql"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
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
