package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewMultiDiscAttachmentCommits(
	database dbapi.DB, now func() time.Time,
) *libraryservice.MultiDiscAttachmentCommits {
	return libraryservice.NewMultiDiscAttachmentCommits(
		repository.NewMultiDiscAttachmentFinalization(database), now,
	)
}

func NewMultiDiscAttachmentTerminals(
	database dbapi.DB, now func() time.Time,
) *libraryservice.MultiDiscAttachmentTerminals {
	return libraryservice.NewMultiDiscAttachmentTerminals(
		repository.NewMultiDiscAttachmentFinalization(database), now,
	)
}

func NewMultiDiscAttachmentSources(database dbapi.DB) *libraryservice.MultiDiscAttachmentSources {
	return libraryservice.NewMultiDiscAttachmentSources(repository.NewMultiDiscAttachmentWorker(database))
}

func NewMultiDiscAttachments(database dbapi.DB, now func() time.Time) *libraryservice.MultiDiscAttachments {
	return libraryservice.NewMultiDiscAttachments(repository.NewMultiDiscAttachments(database),
		libraryservice.MultiDiscAttachmentOptions{Now: now, StorageAvailable: true})
}
