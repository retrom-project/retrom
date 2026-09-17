package libraryimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"strings"
	"time"
)

// ReviewPreviewValidations coordinates the preview refresh use case. The
// application layer supplies the clock while persistence keeps SQL details and
// transaction lifecycle behind the repository port.
type ReviewPreviewValidations struct {
	repository model.ReviewPreviewValidationRepository
	validation ReviewDraftValidationPort
	now        func() time.Time
}

func NewReviewPreviewValidations(
	repository model.ReviewPreviewValidationRepository,
	validation ReviewDraftValidationPort,
	now func() time.Time,
) *ReviewPreviewValidations {
	if now == nil {
		now = time.Now
	}
	return &ReviewPreviewValidations{repository: repository, validation: validation, now: now}
}

func (service *ReviewPreviewValidations) Refresh(ctx context.Context, itemID string) error {
	if strings.TrimSpace(itemID) == "" || service == nil || service.repository == nil || service.validation == nil {
		return model.ErrInvalid
	}
	draft, err := service.repository.Draft(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read review preview validation draft: %w", err)
	}
	validationPlan, err := service.validation.Resolve(ctx, ReviewDraftValidationRequest{
		ItemID: itemID, TargetPlatformInstanceID: draft.TargetID, DefaultDOSEntry: draft.DOSEntry,
	})
	if err != nil {
		return fmt.Errorf("resolve review preview validation: %w", err)
	}
	if validationPlan.SelectedValidationID == draft.Selected && validationPlan.Create == nil {
		return nil
	}
	if validationPlan.SelectedValidationID == "" {
		return model.ErrInvalid
	}
	if err := service.repository.Commit(ctx, model.ReviewPreviewValidationPlan{
		ItemID: itemID, ValidationID: validationPlan.SelectedValidationID,
		ExpectedVersion: draft.Version, ExpectedSelectedValidation: draft.Selected,
		ExpectedSelectedValidationPrepublishDigest: validationPlan.SelectedValidationPrepublishDigest,
		ExpectedValidationGuard:                    validationPlan.Guard,
		Create:                                     validationPlan.Create, Copy: validationPlan.Copy,
		NowMS: service.now().UnixMilli(),
	}); err != nil {
		return fmt.Errorf("refresh review preview validation: %w", err)
	}
	return nil
}
