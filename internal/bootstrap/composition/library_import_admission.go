package composition

import (
	"database/sql"

	repository "retrom/internal/persistence/libraryimport"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryImportAdmissions(
	database *sql.DB, notifier application.ImportGroupNotifier, options application.ImportAdmissionOptions,
) *application.ImportAdmissions {
	return application.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		tagging.New(tagpersistence.New(database), options.Now), options)
}
