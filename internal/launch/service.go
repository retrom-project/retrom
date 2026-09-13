package launch

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	application "retrom/internal/service/launch"

	"retrom/internal/blobstore"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
)

var (
	ErrBlocked          = application.ErrBlocked
	ErrCredential       = application.ErrCredential
	ErrDOSEntryMissing  = application.ErrDOSEntryMissing
	ErrDOSEntryUnsafe   = application.ErrDOSEntryUnsafe
	ErrSaveIncompatible = application.ErrSaveIncompatible
)

type Capabilities = application.Capabilities

type CreateRequest = application.CreateRequest

type (
	Created              = application.Created
	NetplayCreateRequest = application.NetplayCreateRequest
)

type Service struct {
	validationRuns           *validationWorkerRuns
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
	return &Service{
		database:       database,
		dependencies:   dependencySet,
		credentials:    credentials,
		now:            now,
		validationRuns: newValidationWorkerRuns(),
	}
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

func (service *Service) SaveAccess(ctx context.Context, launchID, capability string) (string, error) {
	result, err := service.sessionQueries().SaveAccess(ctx, launchID, capability)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}
