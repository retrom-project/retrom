package libraryimport

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/cleanup"
	librarypersistence "retrom/internal/persistence/libraryimport"
)

func (service *Service) RecoverAttachmentJobs(ctx context.Context) error {
	if err := service.attachmentExecutions.Recover(ctx); err != nil {
		return fmt.Errorf("recover import attachments: %w", err)
	}
	return nil
}

func (service *Service) startAttachmentQueue(ctx context.Context) {
	service.workerMu.Lock()
	if service.attachmentsStarted || service.workerClosed {
		service.workerMu.Unlock()
		return
	}
	service.attachmentsStarted = true
	service.workerMu.Unlock()
	service.scheduleAttachment(ctx, 0, service.runAttachmentQueue)
}

func (service *Service) runAttachmentQueue(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := service.RecoverAttachmentJobs(ctx); err != nil {
			cleanup.Error("recover attachments", err)
		} else if jobs, err := service.attachmentExecutions.Queued(ctx); err != nil {
			cleanup.Error("read attachment queue", err)
		} else {
			for _, job := range jobs {
				service.ResumeAttachmentJob(ctx, job.Kind, job.ID)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ResumeAttachmentJob wakes one durable job; claim fences duplicate notifications.
func (service *Service) ResumeAttachmentJob(ctx context.Context, kind, jobID string) {
	service.scheduleAttachment(ctx, 0, func(worker context.Context) {
		switch kind {
		case "REVIEW_ARCADE_PARENT_VALIDATE":
			service.runParentAttachment(worker, jobID)
		case "REVIEW_MULTI_DISC_VALIDATE":
			service.runMultiDiscAttachment(worker, jobID)
		}
	})
}

func (service *Service) attachmentLease(
	parent context.Context, jobID, workerID string,
) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := librarypersistence.HeartbeatAttachment(ctx, service.database, jobID, workerID, service.now().UnixMilli())
				if err != nil {
					cleanup.Error("renew attachment lease", err)
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}

// Terminal persistence must survive cancellation of expensive work.
func attachmentCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
