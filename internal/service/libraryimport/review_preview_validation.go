package libraryimport

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ReviewPreviewValidations coordinates the preview refresh use case. The
// application layer supplies the clock while persistence keeps SQL details and
// transaction lifecycle behind the repository port.
type ReviewPreviewValidations struct {
	repository ReviewPreviewValidationRepository
	now        func() time.Time
}

func NewReviewPreviewValidations(
	repository ReviewPreviewValidationRepository,
	now func() time.Time,
) *ReviewPreviewValidations {
	if now == nil {
		now = time.Now
	}
	return &ReviewPreviewValidations{repository: repository, now: now}
}

func (service *ReviewPreviewValidations) Refresh(ctx context.Context, itemID string) error {
	if strings.TrimSpace(itemID) == "" || service == nil || service.repository == nil {
		return ErrInvalid
	}
	if err := service.repository.Refresh(ctx, itemID, service.now().UnixMilli()); err != nil {
		return fmt.Errorf("refresh review preview validation: %w", err)
	}
	return nil
}
