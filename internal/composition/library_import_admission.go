package composition

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryImportAdmissions(
	database dbapi.DB, notifier application.ImportGroupNotifier, options application.ImportAdmissionOptions,
) *application.ImportAdmissions {
	return application.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		tagging.New(tagpersistence.New(database), options.Now), options)
}
