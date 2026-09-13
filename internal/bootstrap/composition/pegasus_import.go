package composition

import (
	"database/sql"
	"log/slog"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/imports/pegasusimport"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/repo/dberrors"
	repository "retrom/internal/repo/pegasusimport"
	tagrepository "retrom/internal/repo/tagging"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"
	"retrom/internal/service/tagging"
)

func NewPegasusImport(database *sql.DB, blobs *blobstore.Store, importer application.ReviewSourceCreator,
	credentials *retromruntime.Credentials, roots []serversource.Root, now func() time.Time,
) *application.Service {
	source := pegasusimport.NewSources(blobs, credentials, roots)
	tags := tagging.New(tagrepository.New(database), now)
	items := application.NewItemWork(repository.NewItemWork(database), now)
	material := application.NewMaterialization(repository.NewMaterialization(database), now)
	metadata := library.NewMetadataSeeder(nil, now)
	settlement := application.NewWorkerSettlement(repository.NewWorkerSettlement(database), metadata, now)
	lifecycle := application.NewPlanLifecycle(repository.NewPlanLifecycle(database), now)
	report := func(err error) { slog.Error("Pegasus worker failed", "error", source.Sanitize(err)) }
	dispatcher := application.NewWorkDispatcher(application.WorkDispatchDependencies{
		Sources: source, Publication: application.NewScanPublication(repository.NewScanPublication(database), now),
		Settlement: settlement, Report: report,
		Import: application.ImportExecutorDependencies{
			Items: items, Materials: material,
			Reviews: application.NewReviewPreparation(importer, items,
				application.NewReviewHandoff(repository.NewReviewHandoff(database), metadata, now)),
			Companions: application.NewCompanions(repository.NewCompanions(database), now), Settlement: settlement,
			Completion:  application.NewCompletion(repository.NewCompletion(database), now),
			Diagnostics: pegasusDiagnostics{source},
		},
	})
	worker := application.NewWorker(application.WorkerDependencies{
		Leases: application.NewLeases(repository.NewLeases(database), now), Executor: dispatcher,
		Maintenance: application.NewMaintenance(
			application.NewRecovery(repository.NewRecovery(database), metadata, now), lifecycle),
		Cancellation: application.NewWorkerCancellation(material, settlement), Report: report,
	})
	return application.New(application.ServiceDependencies{
		Queries:   application.NewQueries(repository.NewQueries(database), tags),
		Creation:  application.NewCreation(repository.NewCreation(database), source, now),
		Mappings:  application.NewMappings(repository.NewMappings(database), tags, now),
		Starter:   application.NewStarter(repository.NewStarter(database), source, now),
		Control:   application.NewWorkflowControl(repository.NewWorkflowControl(database), now),
		Lifecycle: lifecycle, Worker: worker,
	})
}

type pegasusDiagnostics struct{ source *pegasusimport.Sources }

func (diagnostics pegasusDiagnostics) Sanitize(err error) string {
	return diagnostics.source.Sanitize(err)
}
func (pegasusDiagnostics) DatabaseCause(err error) string { return dberrors.Classify(err) }
