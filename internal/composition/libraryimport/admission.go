package libraryimport

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewImportAdmissions(
	database dbapi.DB, notifier libraryservice.ImportGroupNotifier, tags *tagging.Service,
	options libraryservice.ImportAdmissionOptions,
) *libraryservice.ImportAdmissions {
	return libraryservice.NewImportAdmissions(repository.NewImportAdmissions(database), notifier,
		tags, options)
}
