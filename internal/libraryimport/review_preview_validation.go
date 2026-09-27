package libraryimport

import (
	"context"
	"fmt"
)

// RefreshReviewPreviewValidation resolves current runtime dependencies without
// resubmitting metadata. Imported metadata may exceed the editor's write limits.
func (service *Service) RefreshReviewPreviewValidation(ctx context.Context, itemID string) error {
	err := service.previewValidations.Refresh(ctx, itemID)
	if err != nil {
		return fmt.Errorf("refresh review preview validation: %w", err)
	}
	return nil
}
