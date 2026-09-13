package libraryimport

import (
	"context"

	"retrom/internal/cleanup"
	composition "retrom/internal/composition/libraryimport"
)

func (service *Service) workerBundle() *composition.WorkerBundle {
	service.workerMu.Lock()
	defer service.workerMu.Unlock()
	if service.worker == nil {
		bundle := composition.NewWorker(
			service.database,
			service.now,
			service.creationDependencies(),
			func(err error) { cleanup.Error("ordinary import worker", err) },
		)
		service.worker = &bundle
		if service.workerClosed {
			bundle.Worker.Close()
		}
	}
	return service.worker
}
func (service *Service) Start() { service.workerBundle().Worker.Start() }
func (service *Service) Close() {
	service.workerMu.Lock()
	service.workerClosed = true
	worker := service.worker
	service.workerMu.Unlock()
	if worker != nil {
		worker.Worker.Close()
	}
}

func (service *Service) NotifyImportGroup(ctx context.Context, id string) {
	service.workerBundle().Worker.NotifyImportGroup(ctx, id)
}

func (service *Service) ResumeImportGroupJobs(ctx context.Context) {
	service.workerBundle().Worker.Resume(ctx)
}

func (service *Service) RecoverImportGroupJobs(ctx context.Context) {
	cleanup.Error("recover ordinary imports", service.workerBundle().Worker.Recover(ctx))
}
func (service *Service) CancelImportGroupJob(id string) { service.workerBundle().Worker.Cancel(id) }
func (service *Service) SyncImportGroupCancellation(ctx context.Context, id string) {
	cleanup.Error(
		"synchronize ordinary import cancellation",
		service.workerBundle().Executions.SyncCancellation(ctx, id),
	)
}
