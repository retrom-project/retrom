package launch

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"retrom/internal/cleanup"

	application "retrom/internal/service/launch"

	"retrom/internal/blobstore"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
)

type Service struct {
	validationRuns           *application.ValidationSupervisor
	database                 *sql.DB
	dependencies             *dependencies.Set
	credentials              *retromruntime.Credentials
	blobs                    *blobstore.Store
	rpgRuntimeOriginTemplate string
	now                      func() time.Time
	runtimeCatalog           runtimecatalog.Catalog
	runtimeBuilder           *runtimelaunch.Builder
	publicOrigin             string
}

func (service *Service) WithRuntimeProvider(
	catalog runtimecatalog.Catalog,
	builder *runtimelaunch.Builder,
) *Service {
	service.runtimeCatalog = catalog
	service.runtimeBuilder = builder
	return service
}

func New(
	database *sql.DB,
	dependencySet *dependencies.Set,
	credentials *retromruntime.Credentials,
	now func() time.Time,
) *Service {
	service := &Service{
		database:     database,
		dependencies: dependencySet,
		credentials:  credentials,
		now:          now,
	}
	service.validationRuns = application.NewValidationSupervisor(testValidationRunner{service}, func(err error) { cleanup.Error("variant validation", err) })
	return service
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithRPGRuntimeOriginTemplate(template string) *Service {
	service.rpgRuntimeOriginTemplate = template
	return service
}

func (service *Service) WithPublicOrigin(origin string) *Service {
	service.publicOrigin = origin
	return service
}

func (service *Service) AuthorizeSave(ctx context.Context, launchID, capability string) error {
	err := service.sessionQueries().AuthorizeSave(ctx, launchID, capability)
	if err != nil {
		return fmt.Errorf("launch resource query: %w", err)
	}
	return nil
}

func (service *Service) sources() *Sources {
	return NewSources(service.blobs, service.credentials).WithRuntimeProvider(service.runtimeBuilder).WithRPGRuntimeOriginTemplate(service.rpgRuntimeOriginTemplate)
}
