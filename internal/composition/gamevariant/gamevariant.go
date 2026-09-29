package gamevariant

import (
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	repository "retrom/internal/persistence/gamevariant"
	importcatalog "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/gamevariant"
	libraryservice "retrom/internal/service/libraryimport"
)

func New(database dbapi.DB, provider application.Provider, now func() time.Time,
	blobs ...*filestore.Store,
) *application.Service {
	worker := application.NewValidationWorker(
		repository.NewValidationWorker(database), application.ValidationWorkerEnvironment{Now: now},
	)
	supervisor := application.NewValidationSupervisor(worker, func(err error) { cleanup.Error("variant validation", err) })
	var arcade *libraryservice.ImportPreparation
	if len(blobs) != 0 && blobs[0] != nil {
		arcade = libraryservice.NewImportPreparation(nil, importcatalog.BindPreparationCatalog(database),
			blobs[0], libraryservice.ImportPreparationOptions{})
	}
	return application.New(repository.New(database), provider, now, supervisor, arcade)
}
