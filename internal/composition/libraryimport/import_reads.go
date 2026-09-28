package libraryimport

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewImportReads(database dbapi.DB) *libraryservice.ImportReads {
	return libraryservice.NewImportReads(repository.NewImportReads(database))
}
