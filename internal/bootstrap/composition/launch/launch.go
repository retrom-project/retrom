package launch

import (
	"database/sql"
	"time"

	"retrom/internal/adapter/runtime/launch"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/foundation/cleanup"
	launchmodel "retrom/internal/model/launch"
	repository "retrom/internal/repo/launch"
	launchservice "retrom/internal/service/launch"
)

// New assembles the complete Launch application with one shared validation lifetime.
func New(database *sql.DB, source *launch.Sources, publicOrigin string, now func() time.Time) *launchservice.Service {
	worker := launchservice.NewValidationWorker(
		repository.NewValidationWorker(database),
		launchmodel.ValidationWorkerEnvironment{Now: now},
	)
	supervisor := launchservice.NewValidationSupervisor(
		worker,
		func(err error) { cleanup.Error("variant validation", err) },
	)
	product := launchservice.NewProductCreator(
		repository.NewProductCreation(database),
		source,
		source,
		launchmodel.ProductEnvironment{
			Now: now, SignCapability: source.SignCapability, SignIsolation: source.SignIsolation,
			ResumeValidation: supervisor.Dispatch,
		},
	)
	preview := launchservice.NewPreviewCreator(
		repository.NewPreviewCreation(database),
		source,
		launchmodel.PreviewEnvironment{
			Now: now, SignCapability: source.SignCapability, SignIsolation: source.SignIsolation,
		},
	)
	netplay := launchservice.NewNetplayCreator(
		repository.NewNetplayCreation(database),
		source,
		source,
		launchmodel.NetplayCreationEnvironment{
			Now: now, SignCapability: source.SignCapability,
		},
	)
	return launchservice.New(launchservice.ServiceDependencies{
		Product: product, Preview: preview, Netplay: netplay, Validation: supervisor,
		Config: launchservice.NewConfigIssuer(repository.NewConfig(database), source, launchmodel.ConfigEnvironment{
			Now: now, Matches: retromruntime.MatchesCapability, PublicOrigin: publicOrigin, SignIsolation: source.SignIsolation,
		}),
		Play: launchservice.NewPlayController(repository.NewPlay(database), now, retromruntime.MatchesCapability),
		Content: launchservice.NewContentAccess(
			repository.NewContentQueries(database),
			now,
			retromruntime.MatchesCapability,
		),
		Sessions: launchservice.NewSessionQueries(
			repository.NewSessionQueries(database),
			source,
			now,
			retromruntime.MatchesCapability,
		),
		Projects: launchservice.NewProjectQueries(repository.NewConfig(database), now, retromruntime.MatchesCapability),
		Indexes: launchservice.NewProjectIndexes(
			repository.NewProjectIndexes(database),
			now,
			retromruntime.MatchesCapability,
		),
		Screenshots: launchservice.NewScreenshotSaver(
			repository.NewScreenshots(database),
			source,
			launchmodel.ScreenshotEnvironment{
				Now: now, Matches: retromruntime.MatchesCapability,
			},
		),
	})
}
