package libraryimport

import (
	"context"
	"fmt"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func (service *Service) reviewApprovals() *application.ReviewApprovals {
	return application.NewReviewApprovals(repository.NewReviewApprovals(service.database),
		service.tags, service.now)
}

func (service *Service) Approve(ctx context.Context, itemID string, expectedVersion int64) (Approved, error) {
	return service.ApproveWithDecision(ctx, itemID, expectedVersion, ApprovalDecision{})
}

func (service *Service) ApproveWithReason(
	ctx context.Context, itemID string, expectedVersion int64, reason *string,
) (Approved, error) {
	return service.ApproveWithDecision(ctx, itemID, expectedVersion, ApprovalDecision{Reason: reason})
}

func (service *Service) ApproveWithDecision(
	ctx context.Context, itemID string, expectedVersion int64, decision ApprovalDecision,
) (Approved, error) {
	result, err := service.reviewApprovals().Approve(ctx, libraryimportmodel.ReviewApprovalRequest{
		ItemID:          itemID,
		ExpectedVersion: expectedVersion, Decision: decision,
	})
	if err != nil {
		return Approved{}, fmt.Errorf("approve library review: %w", err)
	}
	return result, nil
}
