package application

import (
	"context"
	"errors"
	"fmt"
)

var ErrClosed = errors.New("application is closed")

// Start owns partial-startup cleanup. A failed or closed graph cannot restart.
func (services *Services) Start(ctx context.Context) error {
	services.lifecycleMu.Lock()
	defer services.lifecycleMu.Unlock()
	if services.closed {
		return ErrClosed
	}
	if services.started {
		return nil
	}
	if err := services.start(ctx); err != nil {
		services.close()
		return err
	}
	services.started = true
	return nil
}

func (services *Services) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start application: %w", err)
	}
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

func (services *Services) Close() {
	services.lifecycleMu.Lock()
	defer services.lifecycleMu.Unlock()
	services.close()
}

func (services *Services) close() {
	if services.closed {
		return
	}
	services.closed = true
	services.ImportDiscards.Close()
	services.SourceImports.Close()
	services.ServerImports.Close()
	services.ReviewBulkApprovals.Close()
	services.Importer.Close()
	services.Variants.Close()
	services.Metadata.Close()
	services.Uploads.Close()
	services.CleanupJobs.Close()
}
