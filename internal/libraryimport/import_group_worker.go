package libraryimport

import (
	"context"

	"retrom/internal/cleanup"
)

func (service *Service) Start() { service.worker.Start() }
func (service *Service) Close() {
	service.Stop()
	service.Wait()
}

func (service *Service) Stop() {
	service.workerMu.Lock()
	service.workerClosed = true
	for _, cancel := range service.attachmentCancels {
		cancel()
	}
	service.workerMu.Unlock()
	if service.worker != nil {
		service.worker.Stop()
	}
}

func (service *Service) Wait() {
	if service.worker != nil {
		service.worker.Wait()
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
