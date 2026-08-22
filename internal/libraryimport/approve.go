package libraryimport

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
)

func (service *Service) Approve(ctx context.Context, itemID string, expectedVersion int64) (Approved, error) {
	return service.ApproveWithDecision(ctx, itemID, expectedVersion, ApprovalDecision{})
}

func (service *Service) ApproveWithReason(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	reason *string,
) (Approved, error) {
	return service.ApproveWithDecision(ctx, itemID, expectedVersion, ApprovalDecision{Reason: reason})
}

func (service *Service) ApproveWithDecision(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	decision ApprovalDecision,
) (Approved, error) {
	return service.approveWithOptions(ctx, itemID, expectedVersion, decision, approvalOptions{})
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) approveWithOptions(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	decision ApprovalDecision,
	options approvalOptions,
) (Approved, error) {
	input, err := validateApprovalInput(itemID, decision)
	if err != nil {
		return Approved{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	run := newApprovalRun(ctx, service, transaction, itemID, expectedVersion, input, options)
	if err := run.execute(); err != nil {
		return Approved{}, err
	}
	return run.approved(), nil
}

type approvalInput struct {
	decision            ApprovalDecision
	decisionReason      any
	metadataSourceKind  string
	metadataSourceRefID string
}

func validateApprovalInput(itemID string, decision ApprovalDecision) (approvalInput, error) {
	input := approvalInput{
		decision: decision, metadataSourceKind: "IMPORT_REVIEW", metadataSourceRefID: itemID,
	}
	if decision.Reason != nil {
		trimmed := strings.TrimSpace(*decision.Reason)
		if trimmed == "" || !validField(trimmed, 500, true) {
			return approvalInput{}, ErrInvalid
		}
		input.decisionReason = trimmed
	}
	if decision.DuplicatePolicy != "" && decision.DuplicatePolicy != "ALLOW_NEW" {
		return approvalInput{}, ErrInvalid
	}
	if decision.DuplicatePolicy == "" && len(decision.AcknowledgedGameIDs) != 0 {
		return approvalInput{}, ErrInvalid
	}
	if decision.SourceKind != "" {
		if decision.SourceKind != "SERVER_PEGASUS_IMPORT" || decision.SourceRefID == "" {
			return approvalInput{}, ErrInvalid
		}
		input.metadataSourceKind, input.metadataSourceRefID = decision.SourceKind, decision.SourceRefID
	} else if decision.SourceRefID != "" || len(decision.ExternalAssets) != 0 {
		return approvalInput{}, ErrInvalid
	}
	if !validExternalAssets(decision.ExternalAssets) {
		return approvalInput{}, ErrInvalid
	}
	return input, nil
}
