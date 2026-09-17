package composition

import (
	"database/sql"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

func NewLibraryImportAdmissions(
	database *sql.DB, notifier libraryimportmodel.ImportGroupNotifier, options libraryimportmodel.ImportAdmissionOptions,
) *libraryimportservice.ImportAdmissions {
	return libraryimportservice.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		newTagService(database, options.Now), options)
}
