package composition

import (
	"database/sql"
	"log/slog"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/imports/emulationstationimport"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/repo/dberrors"
	repository "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	library "retrom/internal/service/libraryimport"
)

func NewEmulationStationImport(database *sql.DB, blobs *blobstore.Store, importer application.ReviewSourceCreator,
	credentials *retromruntime.Credentials, roots []serversource.Root, now func() time.Time,
) *application.Service {
	control := application.NewExecutionControl(repository.NewExecutionControl(database), now)
	source := emulationstationimport.NewSources(
		blobs,
		credentials,
		roots,
		application.NewSourceGuard(control),
		emulationStationDiagnostics{},
	)
	tags := newTagService(database, now)
	items := application.NewItemWork(repository.NewItemWork(database), now)
	materials := application.NewMaterialization(repository.NewMaterialization(database), now)
	metadata := library.NewMetadataSeeder(nil, now)
	handoff := application.NewReviewHandoff(repository.NewReviewHandoff(database), metadata, now)
	companions := application.NewCompanions(repository.NewCompanions(database), source, now)
	reviews := application.NewReviewPreparer(application.ReviewPreparerDependencies{
		Sources: importer, Companions: companions, Items: items, Phases: materials, Handoff: handoff, Diagnostics: source,
	})
	importerExecutor := application.NewImportExecutor(application.ImportExecutorDependencies{
		Items: items, Materials: materials, Sources: source, Reviews: reviews, Control: control,
		Completion: application.NewCompletion(repository.NewCompletion(database), now), Diagnostics: source,
	})
	recovery := application.NewRecovery(repository.NewRecovery(database), now)
	lifecycle := application.NewPlanLifecycle(repository.NewPlanLifecycle(database), now)
	report := func(err error) { slog.Error("EmulationStation worker failed", "error", source.Sanitize(err)) }
	dispatcher := application.NewExecutionDispatcher(application.ExecutionDispatcherDependencies{
		Roots: source, Scans: application.NewScanExecutor(
			source,
			application.NewScanPublication(repository.NewScanPublication(database), now),
		),
		Imports: importerExecutor, Control: control, Recovery: recovery, Report: report,
	}, now)
	worker := application.NewWorker(application.WorkerDependencies{
		Leases:      application.NewLeases(repository.NewLeases(database), now),
		Maintenance: application.NewMaintenance(recovery, lifecycle), Executor: dispatcher, Control: control, Report: report,
	}, now)
	return application.New(application.ServiceDependencies{
		Queries:   application.NewQueries(repository.NewQueries(database), tags),
		Creation:  application.NewCreation(repository.NewCreation(database), source, now),
		Mappings:  application.NewMappings(repository.NewMappings(database), tags, now),
		Starter:   application.NewStarter(repository.NewStarter(database), source, now),
		Control:   application.NewWorkflowControl(repository.NewWorkflowControl(database), source, now),
		Lifecycle: lifecycle, Worker: worker,
	})
}

type emulationStationDiagnostics struct{}

func (emulationStationDiagnostics) DatabaseCause(err error) string { return dberrors.Classify(err) }
