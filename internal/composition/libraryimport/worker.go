package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewExecutions(database dbapi.DB, now func() time.Time) *libraryservice.ImportExecutions {
	return libraryservice.NewImportExecutions(repository.NewImportExecutions(database), now)
}
