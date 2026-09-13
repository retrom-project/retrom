package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewMultiDiscAttachmentCommits(
	database *sql.DB, now func() time.Time,
) *application.MultiDiscAttachmentCommits {
	return application.NewMultiDiscAttachmentCommits(
		repository.NewMultiDiscAttachmentFinalization(database), now,
	)
}

func NewMultiDiscAttachmentTerminals(
	database *sql.DB, now func() time.Time,
) *application.MultiDiscAttachmentTerminals {
	return application.NewMultiDiscAttachmentTerminals(
		repository.NewMultiDiscAttachmentFinalization(database), now,
	)
}

func NewMultiDiscAttachmentSources(database *sql.DB) *application.MultiDiscAttachmentSources {
	return application.NewMultiDiscAttachmentSources(repository.NewMultiDiscAttachmentWorker(database))
}
