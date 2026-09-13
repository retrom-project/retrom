package launch

import (
	"database/sql"
	"time"

	"retrom/internal/adapter/runtime/launch"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/foundation/cleanup"
	repository "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"
)

// New assembles the complete Launch application with one shared validation lifetime.
func New(database *sql.DB, source *launch.Sources, publicOrigin string, now func() time.Time) *application.Service {
	worker := application.NewValidationWorker(
		repository.NewValidationWorker(database),
		application.ValidationWorkerEnvironment{Now: now},
	)
	supervisor := application.NewValidationSupervisor(worker, func(err error) { cleanup.Error("variant validation", err) })
	product := application.NewProductCreator(
		repository.NewProductCreation(database),
		source,
		source,
		application.ProductEnvironment{
			Now: now, SignCapability: source.SignCapability, SignIsolation: source.SignIsolation,
			ResumeValidation: supervisor.Dispatch,
		},
	)
	preview := application.NewPreviewCreator(
		repository.NewPreviewCreation(database),
		source,
		application.PreviewEnvironment{
			Now: now, SignCapability: source.SignCapability, SignIsolation: source.SignIsolation,
		},
	)
	netplay := application.NewNetplayCreator(
		repository.NewNetplayCreation(database),
		source,
		source,
		application.NetplayCreationEnvironment{
			Now: now, SignCapability: source.SignCapability,
		},
	)
	return application.New(application.ServiceDependencies{
		Product: product, Preview: preview, Netplay: netplay, Validation: supervisor,
		Config: application.NewConfigIssuer(repository.NewConfig(database), source, application.ConfigEnvironment{
			Now: now, Matches: retromruntime.MatchesCapability, PublicOrigin: publicOrigin, SignIsolation: source.SignIsolation,
		}),
		Play:    application.NewPlayController(repository.NewPlay(database), now, retromruntime.MatchesCapability),
		Content: application.NewContentAccess(repository.NewContentQueries(database), now, retromruntime.MatchesCapability),
		Sessions: application.NewSessionQueries(
			repository.NewSessionQueries(database),
			source,
			now,
			retromruntime.MatchesCapability,
		),
		Projects: application.NewProjectQueries(repository.NewConfig(database), now, retromruntime.MatchesCapability),
		Indexes:  application.NewProjectIndexes(repository.NewProjectIndexes(database), now, retromruntime.MatchesCapability),
		Screenshots: application.NewScreenshotSaver(
			repository.NewScreenshots(database),
			source,
			application.ScreenshotEnvironment{
				Now: now, Matches: retromruntime.MatchesCapability,
			},
		),
	})
}
