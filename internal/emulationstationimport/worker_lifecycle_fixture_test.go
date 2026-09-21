package emulationstationimport

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	persistence "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

type workerAdapter struct{ service *Service }

func (service *Service) newWorker() *application.Worker {
	adapter := workerAdapter{service: service}
	return application.NewWorker(application.WorkerDependencies{
		Leases: adapter, Maintenance: application.NewMaintenance(adapter, adapter), Executor: adapter, Control: adapter,
		Report: func(err error) { slog.Error("EmulationStation worker", "error", err) },
	}, func() time.Time { return service.now() })
}

func (adapter workerAdapter) Claim(ctx context.Context) (application.Execution, bool, error) {
	return adapter.service.claim(ctx)
}

func (adapter workerAdapter) Renew(ctx context.Context, unit application.Execution) (application.LeaseState, error) {
	state, err := application.NewLeases(persistence.NewLeases(adapter.service.database), adapter.service.now).Renew(
		ctx,
		unit,
	)
	if err != nil {
		return application.LeaseLost, fmt.Errorf("renew EmulationStation execution: %w", err)
	}
	return state, nil
}

func (adapter workerAdapter) Observe(ctx context.Context, unit application.Execution) (application.LeaseState, error) {
	state, err := adapter.service.executionControl().Observe(ctx, unit)
	if err != nil {
		return application.LeaseLost, fmt.Errorf("observe EmulationStation execution: %w", err)
	}
	return state, nil
}

func (adapter workerAdapter) CloseCancelled(ctx context.Context, unit application.Execution) (bool, error) {
	return adapter.service.closeCancelled(ctx, unit)
}

func (adapter workerAdapter) Fail(
	ctx context.Context,
	unit application.Execution,
	failure application.ExecutionFailure,
) (string, error) {
	state, err := adapter.service.executionControl().Fail(ctx, unit, failure)
	if err != nil {
		return "", fmt.Errorf("fail EmulationStation execution: %w", err)
	}
	return state, nil
}

func (adapter workerAdapter) Recover(ctx context.Context) error {
	return adapter.service.recoverWork(ctx)
}

func (adapter workerAdapter) Expire(ctx context.Context) error {
	return adapter.service.ExpirePlans(ctx)
}

func (adapter workerAdapter) Execute(ctx context.Context, unit application.Execution) {
	adapter.service.executionDispatcher().Execute(ctx, unit)
}

func (adapter workerAdapter) CheckRoot(unit application.Execution) error {
	return adapter.service.sources().CheckRoot(unit)
}

func (adapter workerAdapter) ForScan(unit application.Execution) (application.ScannerSource, error) {
	return adapter.service.sources().ForScan(unit)
}

func (service *Service) scanExecutor() *application.ScanExecutor {
	return application.NewScanExecutor(workerAdapter{service: service}, service.scanPublication())
}

func (service *Service) executionDispatcher() *application.ExecutionDispatcher {
	return application.NewExecutionDispatcher(application.ExecutionDispatcherDependencies{
		Roots: workerAdapter{service: service}, Scans: service.scanExecutor(), Imports: service.importExecutor(),
		Control: service.executionControl(), Recovery: application.NewRecovery(
			persistence.NewRecovery(service.database),
			service.now,
		),
		Report: func(err error) { slog.Error("EmulationStation execution", "error", err) },
	}, service.now)
}
