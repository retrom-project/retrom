package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"retrom/internal/filestore"

	"github.com/google/uuid"
)

type ReviewApprovals struct {
	files      *filestore.Store
	repository ReviewApprovalRepository
	tags       ReviewApprovalTags
	now        func() time.Time
	newID      func() (string, error)
}

func NewReviewApprovals(
	repository ReviewApprovalRepository, tags ReviewApprovalTags, now func() time.Time, files *filestore.Store,
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
	for {
		result, err := service.approveOnce(ctx, request)
		if !errors.Is(err, ErrPublicationBusy) {
			return result, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ReviewApproved{}, fmt.Errorf("wait for content publication: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (service *ReviewApprovals) approveOnce(
	ctx context.Context, request ReviewApprovalRequest,
) (ReviewApproved, error) {
	unlock, err := service.files.LockPublication(ctx, request.ItemID)
	if err != nil {
		return ReviewApproved{}, fmt.Errorf("wait for item publication: %w", err)
	}
	defer unlock()
	state, err := service.preparePublication(ctx, request)
	if err != nil {
		return ReviewApproved{}, err
	}
	if state.State == "PUBLISHED" || state.State == "SKIPPED_EXISTING" {
		return ReviewApproved{GameID: state.GameID, Status: state.State}, nil
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
