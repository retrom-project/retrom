package composition

import (
	"database/sql"
	"log/slog"

	datsource "retrom/internal/adapter/format/arcadedat"
	"retrom/internal/adapter/runtime/dependencies"
	cleanupadapter "retrom/internal/adapter/system/cleanup"
	dependencypersistence "retrom/internal/repo/dependencies"
	dependencyservice "retrom/internal/service/dependencies"
)

func NewDependencyCatalogs(
	set *dependencies.Set, database *sql.DB, logger *slog.Logger,
) *dependencyservice.CatalogService {
	return dependencyservice.NewCatalogs(
		set, dependencypersistence.New(database), datsource.Source{}, cleanupadapter.NewReporter(logger),
	)
}
