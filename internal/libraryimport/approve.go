package libraryimport

import (
	"context"
	"fmt"

	application "retrom/internal/service/libraryimport"
)

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
	result, err := service.approvals.Approve(ctx, application.ReviewApprovalRequest{
		ItemID:          itemID,
		ExpectedVersion: expectedVersion, Decision: decision,
	})
	if err != nil {
		return Approved{}, fmt.Errorf("approve library review: %w", err)
	}
	return result, nil
}
