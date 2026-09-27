package launch

import (
	"context"

	variantrepository "retrom/internal/persistence/gamevariant"
	gamevariant "retrom/internal/service/gamevariant"
)

func (service *Service) validationWorker() *gamevariant.ValidationWorker {
	return gamevariant.NewValidationWorker(variantrepository.NewValidationWorker(service.database), gamevariant.ValidationWorkerEnvironment{Now: service.now})
}

func (service *Service) resumeValidationJob(ctx context.Context, id string) {
	service.validationRuns.Resume(ctx, id)
}
func (service *Service) Close() { service.validationRuns.Close() }

type testValidationRunner struct{ service *Service }

func (runner testValidationRunner) Run(ctx context.Context, id string) error {
	return runner.service.validationWorker().Run(ctx, id)
}

func (runner testValidationRunner) Recover(ctx context.Context) ([]string, error) {
	return runner.service.validationWorker().Recover(ctx)
}
