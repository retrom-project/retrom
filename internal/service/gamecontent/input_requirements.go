package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) inspectReplacementRequirements(ctx context.Context, snapshot JobSnapshot,
	content UploadedFile, prepared *PreparedReplacement,
) error {
	if snapshot.ContentPolicy.Requirements == nil {
		return nil
	}
	file, err := service.blobs.OpenRecord(content.FileRecord)
	if err != nil {
		return fmt.Errorf("read replacement requirements: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	facts, rejection, err := snapshot.ContentPolicy.Requirements.Inspect(ctx, file, content.SizeBytes, content.LogicalName)
	if err != nil {
		return fmt.Errorf("inspect replacement requirements: %w", err)
	}
	if rejection != nil {
		return &replacementValidationError{code: rejection.Code}
	}
	prepared.ContentFacts = facts
	return nil
}
