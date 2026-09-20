package dependencies

import (
	"retrom/internal/adapter/runtime/dependencies"
	model "retrom/internal/model/dependencies"
	"retrom/internal/model/diagnostics"
)

// CatalogService owns DAT indexing independently of BIOS definition seeding.
type CatalogService struct {
	set        *dependencies.Set
	repository model.Repository
	source     model.DATCatalogSource
	reporter   diagnostics.ErrorReporter
}

func NewCatalogs(
	set *dependencies.Set,
	repository model.Repository,
	source model.DATCatalogSource,
	reporter diagnostics.ErrorReporter,
) *CatalogService {
	if source == nil || reporter == nil {
		panic("dependency catalogs require a source and diagnostic reporter")
	}
	return &CatalogService{set: set, repository: repository, source: source, reporter: reporter}
}
