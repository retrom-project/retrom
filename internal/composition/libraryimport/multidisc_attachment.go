package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewMultiDiscAttachmentCommits(
	database dbapi.DB, now func() time.Time,
) *application.MultiDiscAttachmentCommits {
	return application.NewMultiDiscAttachmentCommits(
		repository.NewMultiDiscAttachmentFinalization(database), now,
	)
}

func NewMultiDiscAttachmentTerminals(
	database dbapi.DB, now func() time.Time,
) *application.MultiDiscAttachmentTerminals {
	return application.NewMultiDiscAttachmentTerminals(
		repository.NewMultiDiscAttachmentFinalization(database), now,
	)
}

func NewMultiDiscAttachmentSources(database dbapi.DB) *application.MultiDiscAttachmentSources {
	return application.NewMultiDiscAttachmentSources(repository.NewMultiDiscAttachmentWorker(database))
}
