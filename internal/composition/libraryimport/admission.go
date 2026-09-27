package libraryimport

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewImportAdmissions(
	database dbapi.DB, notifier application.ImportGroupNotifier, tags *tagging.Service,
	options application.ImportAdmissionOptions,
) *application.ImportAdmissions {
	return application.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		tags, options)
}
