package launch

import (
	"context"

	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

func (service *Service) validationWorker() *application.ValidationWorker {
	return application.NewValidationWorker(persistence.NewValidationWorker(service.database), application.ValidationWorkerEnvironment{Now: service.now})
}

func (service *Service) resumeValidationJob(ctx context.Context, id string) {
	service.validationRuns.Resume(ctx, id)
}
func (service *Service) ResumeQueuedValidationJobs() { service.validationRuns.Recover() }
func (service *Service) Close()                      { service.validationRuns.Close() }

type testValidationRunner struct{ service *Service }

func (runner testValidationRunner) Run(ctx context.Context, id string) error {
	return runner.service.validationWorker().Run(ctx, id)
}

func (runner testValidationRunner) Recover(ctx context.Context) ([]string, error) {
	return runner.service.validationWorker().Recover(ctx)
}
