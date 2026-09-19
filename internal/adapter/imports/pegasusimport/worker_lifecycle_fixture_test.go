package pegasusimport

import (
	"context"
	"fmt"
	"log/slog"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	repository "retrom/internal/repo/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) backgroundWorker() *application.Worker {
	service.workerOnce.Do(func() {
		adapter := workerAdapter{service: service}
		service.worker = application.NewWorker(application.WorkerDependencies{
			Leases:      application.NewLeases(repository.NewLeases(service.database), service.now),
			Maintenance: adapter, Executor: adapter,
			Cancellation: workerCancellation{observer: service.materialization(), settlement: service.workerSettlement()},
			Report:       func(err error) { slog.Error("Pegasus worker failed", "error", service.sanitizeTechnicalDetail(err)) },
		})
	})
	return service.worker
}

type workerAdapter struct{ service *Service }

func (adapter workerAdapter) Maintain(ctx context.Context) error {
	if err := adapter.service.maintain(ctx); err != nil {
		return fmt.Errorf("maintain Pegasus plans: %w", err)
	}
	return nil
}

func (adapter workerAdapter) Execute(ctx context.Context, unit pegasusimportmodel.Work) {
	adapter.service.dispatcher().Execute(ctx, unit)
}

type workerCancellation struct {
	observer   *application.Materialization
	settlement *application.WorkerSettlement
}

func (cancellation workerCancellation) Cancelled(ctx context.Context, id pegasusimportmodel.ExecutionIdentity) (bool, error) {
	pending, err := cancellation.observer.Cancelled(ctx, id)
	if err != nil {
		return false, fmt.Errorf("observe Pegasus cancellation: %w", err)
	}
	return pending, nil
}

func (cancellation workerCancellation) CloseCancelled(
	ctx context.Context, id pegasusimportmodel.ExecutionIdentity,
) (bool, error) {
	closed, err := cancellation.settlement.Cancelled(ctx, id)
	if err != nil {
		return false, fmt.Errorf("settle Pegasus cancellation: %w", err)
	}
	return closed, nil
}
