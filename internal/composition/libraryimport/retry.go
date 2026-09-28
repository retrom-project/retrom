package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewImportItemRetries(database dbapi.DB, now func() time.Time) *libraryservice.ImportItemRetries {
	return libraryservice.NewImportItemRetries(repository.NewImportItemRetries(database), now)
}
