package gamevariant

import (
	"time"

	arcaderecords "retrom/internal/persistence/arcade"

	"retrom/internal/cleanup"
	"retrom/internal/content/arcade"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	repository "retrom/internal/persistence/gamevariant"
	application "retrom/internal/service/gamevariant"
)

func New(database dbapi.DB, provider application.Provider, now func() time.Time,
	blobs *filestore.Store,
) *application.Service {
	worker := application.NewValidationWorker(
		repository.NewValidationWorker(database), application.ValidationWorkerEnvironment{Now: now},
	)
	supervisor := application.NewValidationSupervisor(worker, func(err error) { cleanup.Error("variant validation", err) })
	preparation := arcade.NewPreparation(arcaderecords.New(database), archiveFiles{store: blobs})
	return application.New(repository.New(database), provider, now, supervisor, preparation)
}
