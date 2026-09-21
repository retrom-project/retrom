package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewImportItemRetries(database *sql.DB, now func() time.Time) *application.ImportItemRetries {
	return application.NewImportItemRetries(repository.NewImportItemRetries(database), now)
}
