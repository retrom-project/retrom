package gamevariant

import (
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/gamevariant"
	application "retrom/internal/service/gamevariant"
)

func New(database dbapi.DB, provider application.Provider, now func() time.Time) *application.Service {
	worker := application.NewValidationWorker(
		repository.NewValidationWorker(database), application.ValidationWorkerEnvironment{Now: now},
	)
	supervisor := application.NewValidationSupervisor(worker, func(err error) { cleanup.Error("variant validation", err) })
	return application.New(repository.New(database), provider, now, supervisor)
}
