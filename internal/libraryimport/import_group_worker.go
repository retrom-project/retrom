package libraryimport

import (
	"context"

	"retrom/internal/cleanup"
)

func (service *Service) Start() { service.worker.Start() }
func (service *Service) Close() {
	service.workerMu.Lock()
	service.workerClosed = true
	worker := service.worker
	for _, cancel := range service.attachmentCancels {
		cancel()
	}
	service.workerMu.Unlock()
	if worker != nil {
		worker.Close()
	}
	service.attachments.Wait()
}

func (service *Service) NotifyImportGroup(ctx context.Context, id string) {
	service.worker.NotifyImportGroup(ctx, id)
}

func (service *Service) ResumeImportGroupJobs(ctx context.Context) {
	service.worker.Resume(ctx)
}

func (service *Service) RecoverImportGroupJobs(ctx context.Context) {
	cleanup.Error("recover ordinary imports", service.worker.Recover(ctx))
}
func (service *Service) CancelImportGroupJob(id string) { service.worker.Cancel(id) }
func (service *Service) SyncImportGroupCancellation(ctx context.Context, id string) {
	cleanup.Error(
		"synchronize ordinary import cancellation",
		service.executions.SyncCancellation(ctx, id),
	)
}
