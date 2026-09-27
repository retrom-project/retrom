package libraryimport

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/filestore"

	"github.com/google/uuid"

	"retrom/internal/service/tagging"
)

type ReviewApprovals struct {
	files      *filestore.Store
	repository ReviewApprovalRepository
	tags       *tagging.Service
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewApprovals(
	repository ReviewApprovalRepository, tags *tagging.Service, now func() time.Time, files *filestore.Store,
) *ReviewApprovals {
	return &ReviewApprovals{files: files, repository: repository, tags: tags, now: now, newID: newReviewApprovalID}
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
	if service.files == nil {
		return ReviewApproved{}, ErrInvalid
	}
	unlock := service.files.LockPublication()
	defer unlock()
	state, err := service.preparePublication(ctx, request)
	if err != nil {
		return ReviewApproved{}, err
	}
	if state.State == "PUBLISHED" {
		return ReviewApproved{GameID: state.GameID, Status: "PUBLISHED"}, nil
	}
	if state.Intent == nil {
		return ReviewApproved{}, ErrInvalid
	}
	assets, err := service.publishDirectory(ctx, *state.Intent)
	if err != nil {
		return ReviewApproved{}, err
	}
	return service.completePublication(ctx, *state.Intent, assets)
}
