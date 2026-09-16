package composition

import (
	"database/sql"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewLibraryImportAdmissions(
	database *sql.DB, notifier application.ImportGroupNotifier, options application.ImportAdmissionOptions,
) *application.ImportAdmissions {
	return application.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		newTagService(database, options.Now), options)
}
