package metadatascrape

import (
	"context"
	"fmt"

	model "retrom/internal/model/metadatascrape"
)

type InitialReviewService struct{ scope model.InitialReviewScope }

func NewInitialReview(scope model.InitialReviewScope) *InitialReviewService {
	return &InitialReviewService{scope: scope}
}

func (service *InitialReviewService) Complete(ctx context.Context, runID string, now int64) error {
	item, active, err := service.active(ctx, runID)
	if err != nil || !active {
		return err
	}
	if err := service.applyCandidate(ctx, runID, item.ItemID, now); err != nil {
		return err
	}
	return service.advanceReview(ctx, item, now)
}

func (service *InitialReviewService) advanceReview(ctx context.Context, item model.InitialImport, now int64) error {
	change := model.InitialProgress(item, now)
	change.ItemState = "REVIEW_PENDING"
	change.ReviewDelta = 1
	change.JobState = "RUNNING"
	if item.Running == 1 {
		change.JobState = "REVIEW_PENDING"
		if item.Failed > 0 || item.Rejected > 0 {
			change.JobState = "PARTIAL_FAILURE"
		}
	}
	if err := service.scope.Write.Advance(ctx, change); err != nil {
		return fmt.Errorf("complete initial review progress: %w", err)
	}
	return nil
}

func (service *InitialReviewService) Fail(ctx context.Context, runID, code string, now int64) error {
	item, active, err := service.active(ctx, runID)
	if err != nil || !active {
		return err
	}
	change := model.InitialProgress(item, now)
	stage := "SCRAPING"
	change.ItemState = "FAILED_RETRYABLE"
	change.FailedDelta = 1
	change.FailedStage = &stage
	change.ErrorCode = &code
	change.JobState = "RUNNING"
	if item.Running == 1 {
		change.JobState = "PARTIAL_FAILURE"
	}
	if err := service.scope.Write.Advance(ctx, change); err != nil {
		return fmt.Errorf("fail initial review progress: %w", err)
	}
	return nil
}

func (service *InitialReviewService) Cancel(ctx context.Context, runID string, parentCancelled bool, now int64) error {
	item, active, err := service.active(ctx, runID)
	if err != nil || !active {
		return err
	}
	if !parentCancelled {
		return service.advanceReview(ctx, item, now)
	}
	change := model.InitialProgress(item, now)
	change.ItemState = "CANCELLED"
	change.CancelledDelta = 1
	change.JobState = "CANCEL_REQUESTED"
	if item.Running == 1 {
		change.JobState = "CANCELLED"
	}
	if err := service.scope.Write.Advance(ctx, change); err != nil {
		return fmt.Errorf("cancel initial scrape progress: %w", err)
	}
	return nil
}

func (service *InitialReviewService) active(ctx context.Context, runID string) (model.InitialImport, bool, error) {
	item, found, err := service.scope.Read.Import(ctx, runID)
	if err != nil {
		return model.InitialImport{}, false, fmt.Errorf("read initial scrape owner: %w", err)
	}
	if !found || item.ItemState != "SCRAPING" {
		return item, false, nil
	}
	if item.Running < 1 {
		return model.InitialImport{}, false, model.ErrInitialProgressState
	}
	return item, true, nil
}

func (service *InitialReviewService) applyCandidate(ctx context.Context, runID, itemID string, now int64) error {
	candidates, err := service.scope.Read.Candidates(ctx, runID)
	if err != nil {
		return fmt.Errorf("read initial scrape candidates: %w", err)
	}
	candidate, found := model.SelectInitialCandidate(candidates)
	if !found {
		return nil
	}
	draft, err := service.scope.Read.Draft(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read initial review draft: %w", err)
	}
	metadata, title, err := model.MergeInitialReviewMetadata(draft.MetadataJSON, candidate.MetadataJSON)
	if err != nil {
		return fmt.Errorf("merge initial review metadata: %w", err)
	}
	assets, err := service.scope.Read.ReadyAssets(ctx, candidate.ID)
	if err != nil {
		return fmt.Errorf("read initial candidate assets: %w", err)
	}
	change := model.InitialDraftChange{
		ItemID: itemID, DraftID: draft.ID, CandidateID: candidate.ID,
		MetadataJSON: metadata, Title: title, Now: now,
	}
	model.SelectInitialAssets(&change, assets)
	if err := service.scope.Write.Apply(ctx, change); err != nil {
		return fmt.Errorf("apply initial scrape candidate: %w", err)
	}
	return nil
}
