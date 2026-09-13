package composition

import (
	"database/sql"

	repository "retrom/internal/repo/libraryimport"
	tagpersistence "retrom/internal/repo/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryImportAdmissions(
	database *sql.DB, notifier application.ImportGroupNotifier, options application.ImportAdmissionOptions,
) *application.ImportAdmissions {
	return application.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		tagging.New(tagpersistence.New(database), options.Now), options)
}
