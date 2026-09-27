package cleanupjobs

import "context"

func (service *Service) Start()  { service.worker.Start() }
func (service *Service) Close()  { service.worker.Close() }
func (service *Service) Signal() { service.worker.Signal() }
func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	return service.worker.RunOnce(ctx)
}
func (service *Service) Recover(ctx context.Context) error { return service.worker.Recover(ctx) }

func (service *Service) StageInScope(ctx context.Context, scope DeletionScope, ids []string) error {
	return service.deletion.StageInScope(ctx, scope, ids)
}
