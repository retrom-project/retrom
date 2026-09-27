package libraryimport

import (
	"context"
	"time"
)

// Attachment work retains the request actor but belongs to the importer lifetime.
// Shutdown cancels both active work and delayed retries, then waits for them.
func (service *Service) scheduleAttachment(parent context.Context, delay time.Duration, run func(context.Context)) {
	service.workerMu.Lock()
	defer service.workerMu.Unlock()
	if service.workerClosed {
		return
	}
	if service.attachmentCancels == nil {
		service.attachmentCancels = make(map[uint64]context.CancelFunc)
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	service.nextAttachmentID++
	id := service.nextAttachmentID
	service.attachmentCancels[id] = cancel
	service.attachments.Add(1)
	go func() {
		defer service.attachments.Done()
		defer cancel()
		defer func() {
			service.workerMu.Lock()
			delete(service.attachmentCancels, id)
			service.workerMu.Unlock()
		}()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() == nil {
			run(ctx)
		}
	}()
}
