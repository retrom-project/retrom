package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReconfigurations(database dbapi.DB, now func() time.Time,
	files *filestore.Store, creations *application.ImportCreations,
) *application.Reconfigurations {
	return application.NewReconfigurations(repository.NewReconfigurations(database),
		creations.Create, files.CopyTo, files.RemovePath, now)
}
