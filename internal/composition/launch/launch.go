package launch

import (
	"context"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/launch"
	repository "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	gamevariant "retrom/internal/service/gamevariant"
	application "retrom/internal/service/launch"
)

// New assembles runtime use cases; the process owns variant validation separately.
func New(
	database dbapi.DB, source *launch.Sources, publicOrigin string,
	now func() time.Time, dispatch func(context.Context, string),
	prepareArcade ...func(context.Context, gamevariant.Snapshot) (*gamevariant.ArcadePreparation, error),
) *application.Service {
	var prepare func(context.Context, gamevariant.Snapshot) (*gamevariant.ArcadePreparation, error)
	if len(prepareArcade) != 0 {
		prepare = prepareArcade[0]
	}
	product := application.NewProductCreator(
		repository.NewProductCreation(database),
		source,
		source,
		application.ProductEnvironment{
			Now: now, SignCapability: source.SignCapability, SignIsolation: source.SignIsolation,
			ResumeValidation: dispatch,
			PrepareArcade:    prepare,
		},
	)
	return application.New(application.ServiceDependencies{
		Product: product,
		Config: application.NewConfigIssuer(repository.NewConfig(database), source, application.ConfigEnvironment{
			Now: now, Matches: retromruntime.MatchesCapability, PublicOrigin: publicOrigin, SignIsolation: source.SignIsolation,
		}),
		PreviewCloser: application.NewPreviewCloser(
			repository.NewPreviewClose(database), now, retromruntime.MatchesCapability,
		),
		Play: application.NewPlayController(repository.NewPlay(database), now, retromruntime.MatchesCapability),
		Content: application.NewContentAccess(
			repository.NewContentQueries(database), now, retromruntime.MatchesCapability,
		),
		Sessions: application.NewSessionQueries(
			repository.NewSessionQueries(database),
			source,
			now,
			retromruntime.MatchesCapability,
		),
		Projects: application.NewProjectQueries(repository.NewConfig(database), now, retromruntime.MatchesCapability),
		Indexes:  application.NewProjectIndexes(repository.NewProjectIndexes(database), now, retromruntime.MatchesCapability),
	})
}
