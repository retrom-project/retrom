package composition

import (
	"database/sql"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	tagpersistence "retrom/internal/repo/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryImportAdmissions(
	database *sql.DB, notifier libraryimportmodel.ImportGroupNotifier, options application.ImportAdmissionOptions,
) *application.ImportAdmissions {
	return application.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		tagging.New(tagpersistence.New(database), options.Now), options)
}
