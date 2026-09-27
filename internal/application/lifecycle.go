package application

import (
	"context"
	"fmt"
)

// Start runs recovery before accepting HTTP traffic. Its caller owns Close even
// when startup fails, so partially started services are always stopped.
func (services *Services) Start(ctx context.Context) error {
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
