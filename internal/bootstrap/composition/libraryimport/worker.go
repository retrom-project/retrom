package libraryimport

import (
	"database/sql"
	"time"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type WorkerBundle struct {
	Executions *application.ImportExecutions
	Worker     *application.ImportWorker
}

func NewWorker(database *sql.DB, now func() time.Time, options CreationOptions, report func(error)) WorkerBundle {
	executions := application.NewImportExecutions(repository.NewImportExecutions(database), now)
	worker := application.NewImportWorker(libraryimportmodel.ImportWorkerDependencies{
		Queue:       executions,
		Control:     executions,
		Recovery:    executions,
		Preparation: NewPreparation(database, options),
		Creations:   NewCreations(database, now, options),
	}, application.ImportWorkerSettings{Now: now, Report: report},
	)
	return WorkerBundle{Executions: executions, Worker: worker}
}
