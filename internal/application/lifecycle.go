package application

import (
	"context"
	"errors"
	"fmt"
)

var ErrClosed = errors.New("application is closed")

// Start initiates partial-startup cleanup on failure. Close/Shutdown joins it; a failed graph cannot restart.
func (services *Services) Start(ctx context.Context) error {
	services.lifecycleMu.Lock()
	if services.stopping.Load() {
		services.lifecycleMu.Unlock()
		return ErrClosed
	}
	if services.started {
		services.lifecycleMu.Unlock()
		return nil
	}
	err := services.start(ctx)
	if err != nil {
		services.stopping.Store(true)
	} else {
		services.started = true
	}
	services.lifecycleMu.Unlock()
	if err != nil {
		services.beginShutdown()
	}
	return err
}

func (services *Services) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start application: %w", err)
	}
	services.catalogs.Start(ctx)
	services.Metadata.Start(ctx)
	if err := services.Variants.Recover(ctx); err != nil {
		return fmt.Errorf("recover variant validation: %w", err)
	}
	services.Importer.Start()
	if err := services.Importer.ResumeParentAttachmentJobs(ctx); err != nil {
		return fmt.Errorf("recover import attachments: %w", err)
	}
	if err := services.Importer.ResumeMultiDiscAttachmentJobs(ctx); err != nil {
		return fmt.Errorf("recover import attachments: %w", err)
	}
	if err := services.ReviewBulkApprovals.Start(ctx); err != nil {
		return fmt.Errorf("recover bulk approvals: %w", err)
	}
	services.ServerImports.Start(ctx)
	services.SourceImports.Start()
	services.ImportDiscards.Start(ctx)
	services.CleanupJobs.Start()
	services.Uploads.Start(ctx)
	return nil
}

// Close waits until every worker has actually exited. Process shutdown supervises this wait.
func (services *Services) Close() {
	services.beginShutdown()
	<-services.shutdown.done
}

// Shutdown bounds the caller's wait, not the workers' lifetime. A timeout is not a safe resource-release barrier.
func (services *Services) Shutdown(ctx context.Context) error {
	services.beginShutdown()
	select {
	case <-services.shutdown.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for application workers %v: %w", services.PendingShutdown(), ctx.Err())
	}
}

func (services *Services) PendingShutdown() []string { return services.shutdown.pendingNames() }

func (services *Services) beginShutdown() {
	services.stopping.Store(true)
	services.shutdown.start(func() {
		// Startup recovery and shutdown cannot mutate worker registration concurrently.
		services.lifecycleMu.Lock()
		services.started = false
		services.lifecycleMu.Unlock()
	})
}
