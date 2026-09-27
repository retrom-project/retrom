package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewExecutions(database dbapi.DB, now func() time.Time) *application.ImportExecutions {
	return application.NewImportExecutions(repository.NewImportExecutions(database), now)
}
