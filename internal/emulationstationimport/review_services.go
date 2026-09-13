package emulationstationimport

import (
	"context"
	"path"
	"strings"

	persistence "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	library "retrom/internal/service/libraryimport"
)

func (service *Service) reviewHandoff() *application.ReviewHandoff {
	return application.NewReviewHandoff(persistence.NewReviewHandoff(service.database),
		library.NewMetadataSeeder(nil, service.now), service.now)
}

func (service *Service) reviewPreparer() *application.ReviewPreparer {
	return application.NewReviewPreparer(application.ReviewPreparerDependencies{
		Sources: service.importer, Companions: reviewCompanionsAdapter{service: service}, Items: service.itemWork(),
		Phases: service.materialization(), Handoff: service.reviewHandoff(), Diagnostics: importExecutorAdapter{
			service: service,
		},
	})
}

type reviewCompanionsAdapter struct{ service *Service }

func (adapter reviewCompanionsAdapter) Files(
	ctx context.Context,
	unit work,
	item executionItem,
) ([]library.ServerSourceFile, error) {
	if item.TargetPlatformKind != "arcade" || len(item.Files) != 1 ||
		!strings.EqualFold(path.Ext(item.Files[0].Path), ".zip") {
		return []library.ServerSourceFile{}, nil
	}
	root, err := importExecutorAdapter(adapter).root(unit)
	if err != nil {
		return nil, err
	}
	return adapter.service.arcadeCompanions(ctx, unit, root, item)
}
