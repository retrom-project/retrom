package emulationstationimport

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

type workerAdapter struct{ service *Service }

func (service *Service) newWorker() *emulationstationimportservice.Worker {
	adapter := workerAdapter{service: service}
	return emulationstationimportservice.NewWorker(emulationstationimportservice.WorkerDependencies{
		Leases: adapter, Maintenance: emulationstationimportservice.NewMaintenance(adapter, adapter), Executor: adapter, Control: adapter,
		Report: func(err error) { slog.Error("EmulationStation worker", "error", err) },
	}, func() time.Time { return service.now() })
}

func (adapter workerAdapter) Claim(ctx context.Context) (emulationstationimportmodel.Execution, bool, error) {
	return adapter.service.claim(ctx)
}

func (adapter workerAdapter) Renew(ctx context.Context, unit emulationstationimportmodel.Execution) (emulationstationimportmodel.LeaseState, error) {
	state, err := emulationstationimportservice.NewLeases(persistence.NewLeases(adapter.service.database), adapter.service.now).Renew(
		ctx,
		unit,
	)
	if err != nil {
		return emulationstationimportmodel.LeaseLost, fmt.Errorf("renew EmulationStation execution: %w", err)
	}
	return state, nil
}

func (adapter workerAdapter) Observe(ctx context.Context, unit emulationstationimportmodel.Execution) (emulationstationimportmodel.LeaseState, error) {
	state, err := adapter.service.executionControl().Observe(ctx, unit)
	if err != nil {
		return emulationstationimportmodel.LeaseLost, fmt.Errorf("observe EmulationStation execution: %w", err)
	}
	return state, nil
}

func (adapter workerAdapter) CloseCancelled(ctx context.Context, unit emulationstationimportmodel.Execution) (bool, error) {
	return adapter.service.closeCancelled(ctx, unit)
}

func (adapter workerAdapter) Fail(
	ctx context.Context,
	unit emulationstationimportmodel.Execution,
	failure emulationstationimportmodel.ExecutionFailure,
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

func (adapter workerAdapter) Execute(ctx context.Context, unit emulationstationimportmodel.Execution) {
	adapter.service.executionDispatcher().Execute(ctx, unit)
}

func (adapter workerAdapter) CheckRoot(unit emulationstationimportmodel.Execution) error {
	return adapter.service.sources().CheckRoot(unit)
}

func (adapter workerAdapter) ForScan(unit emulationstationimportmodel.Execution) (emulationstationimportservice.ScannerSource, error) {
	return adapter.service.sources().ForScan(unit)
}

func (service *Service) scanExecutor() *emulationstationimportservice.ScanExecutor {
	return emulationstationimportservice.NewScanExecutor(workerAdapter{service: service}, service.scanPublication())
}

func (service *Service) executionDispatcher() *emulationstationimportservice.ExecutionDispatcher {
	return emulationstationimportservice.NewExecutionDispatcher(emulationstationimportservice.ExecutionDispatcherDependencies{
		Roots: workerAdapter{service: service}, Scans: service.scanExecutor(), Imports: service.importExecutor(),
		Control: service.executionControl(), Recovery: emulationstationimportservice.NewRecovery(
			persistence.NewRecovery(service.database),
			service.now,
		),
		Report: func(err error) { slog.Error("EmulationStation execution", "error", err) },
	}, service.now)
}
