package launch

import "context"

// ServiceDependencies contains complete use cases assembled at process startup.
type ServiceDependencies struct {
	Product     *ProductCreator
	Preview     *PreviewCreator
	Config      *ConfigIssuer
	Play        *PlayController
	Content     *ContentAccess
	Sessions    *SessionQueries
	Projects    *ProjectQueries
	Indexes     *ProjectIndexes
	Screenshots *ScreenshotSaver
	Validation  *ValidationSupervisor
}

type Service struct{ dependencies ServiceDependencies }

func New(dependencies ServiceDependencies) *Service { return &Service{dependencies: dependencies} }
func (service *Service) Close()                     { service.dependencies.Validation.Close() }
func (service *Service) ResumeValidationJob(ctx context.Context, id string) {
	service.dependencies.Validation.Resume(ctx, id)
}
func (service *Service) ResumeQueuedValidationJobs() { service.dependencies.Validation.Recover() }

func (service *Service) EnsureVariantForMove(ctx context.Context, gameID, coreID string) (Created, error) {
	return service.dependencies.Product.EnsureVariant(ctx, gameID, coreID, Capabilities{
		SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
	})
}
