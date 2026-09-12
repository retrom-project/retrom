package libraryimport

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/service/tagging"
)

type ReviewApprovals struct {
	repository ReviewApprovalRepository
	tags       *tagging.Service
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewApprovals(
	repository ReviewApprovalRepository, tags *tagging.Service, now func() time.Time,
) *ReviewApprovals {
	return &ReviewApprovals{repository: repository, tags: tags, now: now, newID: newReviewApprovalID}
}

func newReviewApprovalID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate review approval ID: %w", err)
	}
	return id.String(), nil
}

func (service *ReviewApprovals) Approve(
	ctx context.Context, request ReviewApprovalRequest,
) (ReviewApproved, error) {
	request, err := normalizeReviewApproval(request)
	if err != nil {
		return ReviewApproved{}, err
	}
	var result ReviewApproved
	err = service.repository.WithApproval(ctx, func(scope ReviewApprovalScope) error {
		var approvalErr error
		result, approvalErr = service.ApproveInScope(ctx, scope, request)
		return approvalErr
	})
	if err != nil {
		return ReviewApproved{}, fmt.Errorf("commit review approval: %w", err)
	}
	return result, nil
}

// ApproveInScope publishes through the caller's transaction. The result is durable only after its commit.
func (service *ReviewApprovals) ApproveInScope(
	ctx context.Context, scope ReviewApprovalScope, request ReviewApprovalRequest,
) (ReviewApproved, error) {
	request, err := normalizeReviewApproval(request)
	if err != nil {
		return ReviewApproved{}, err
	}
	run := reviewApprovalRun{ctx: ctx, service: service, scope: scope, request: request}
	for _, step := range []func() error{run.load, run.prepare, run.claimDuplicates, run.publish} {
		if err := step(); err != nil {
			return ReviewApproved{}, err
		}
	}
	return run.result(), nil
}
