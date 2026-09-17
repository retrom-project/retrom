package libraryimport

import (
	"database/sql"
	"time"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

type WorkerBundle struct {
	Executions *libraryimportservice.ImportExecutions
	Worker     *libraryimportservice.ImportWorker
}

func NewWorker(database *sql.DB, now func() time.Time, options CreationOptions, report func(error)) WorkerBundle {
	executions := libraryimportservice.NewImportExecutions(repository.NewImportExecutions(database), now)
	worker := libraryimportservice.NewImportWorker(
		libraryimportmodel.ImportWorkerDependencies{
			Queue:       executions,
			Control:     executions,
			Recovery:    executions,
			Preparation: NewPreparation(database, options),
			Creations:   NewCreations(database, now, options),
		},
		libraryimportmodel.ImportWorkerSettings{Now: now, Report: report},
	)
	return WorkerBundle{Executions: executions, Worker: worker}
}
