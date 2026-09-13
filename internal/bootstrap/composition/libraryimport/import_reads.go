package libraryimport

import (
	"database/sql"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewImportReads(database *sql.DB) *application.ImportReads {
	return application.NewImportReads(repository.NewImportReads(database))
}
