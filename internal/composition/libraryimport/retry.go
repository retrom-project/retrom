package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewImportItemRetries(database dbapi.DB, now func() time.Time) *application.ImportItemRetries {
	return application.NewImportItemRetries(repository.NewImportItemRetries(database), now)
}
