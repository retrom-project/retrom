package payloadrelease

import (
	"context"
	"fmt"
)

func (service *Service) Start()  { service.worker.Start() }
func (service *Service) Close()  { service.worker.Close() }
func (service *Service) Signal() { service.worker.Signal() }
func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	return service.worker.RunOnce(ctx)
}
func (service *Service) Recover(ctx context.Context) error { return service.worker.Recover(ctx) }

func (service *Service) StageInScope(ctx context.Context, scope GCScope, ids []string) error {
	return service.gc.StageInScope(ctx, scope, ids)
}

func (service *Service) ScheduleImmediateGC(ctx context.Context, actor string) (ImmediateGCResult, error) {
	return service.gc.Immediate(ctx, actor)
}

func (service *Service) GameDeleteImpact(ctx context.Context, id string) (GameImpact, error) {
	impact, err := service.impact.Game(ctx, id)
	if err != nil {
		return GameImpact{}, fmt.Errorf("read game deletion impact: %w", err)
	}
	return impact, nil
}
