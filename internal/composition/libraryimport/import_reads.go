package libraryimport

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewImportReads(database dbapi.DB) *application.ImportReads {
	return application.NewImportReads(repository.NewImportReads(database))
}
