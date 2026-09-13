package metadatascrape

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

type InitialReviewService struct{ scope InitialReviewScope }

func NewInitialReview(scope InitialReviewScope) *InitialReviewService {
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

func (service *InitialReviewService) advanceReview(ctx context.Context, item InitialImport, now int64) error {
	change := initialProgress(item, now)
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
	change := initialProgress(item, now)
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

func (service *InitialReviewService) active(ctx context.Context, runID string) (InitialImport, bool, error) {
	item, found, err := service.scope.Read.Import(ctx, runID)
	if err != nil {
		return InitialImport{}, false, fmt.Errorf("read initial scrape owner: %w", err)
	}
	if !found || item.ItemState != "SCRAPING" {
		return item, false, nil
	}
	if item.Running < 1 {
		return InitialImport{}, false, ErrInitialProgressState
	}
	return item, true, nil
}

func initialProgress(item InitialImport, now int64) InitialProgressChange {
	return InitialProgressChange{
		ItemID:          item.ItemID,
		ImportJobID:     item.ImportJobID,
		ExpectedRunning: item.Running,
		ExpectedVersion: item.Version,
		Now:             now,
	}
}

func (service *InitialReviewService) applyCandidate(ctx context.Context, runID, itemID string, now int64) error {
	candidates, err := service.scope.Read.Candidates(ctx, runID)
	if err != nil {
		return fmt.Errorf("read initial scrape candidates: %w", err)
	}
	candidate, found := selectInitialCandidate(candidates)
	if !found {
		return nil
	}
	draft, err := service.scope.Read.Draft(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read initial review draft: %w", err)
	}
	metadata, title, err := mergeInitialReviewMetadata(draft.MetadataJSON, candidate.MetadataJSON)
	if err != nil {
		return err
	}
	assets, err := service.scope.Read.ReadyAssets(ctx, candidate.ID)
	if err != nil {
		return fmt.Errorf("read initial candidate assets: %w", err)
	}
	change := InitialDraftChange{
		ItemID:       itemID,
		DraftID:      draft.ID,
		CandidateID:  candidate.ID,
		MetadataJSON: metadata,
		Title:        title,
		Now:          now,
	}
	selectInitialAssets(&change, assets)
	if err := service.scope.Write.Apply(ctx, change); err != nil {
		return fmt.Errorf("apply initial scrape candidate: %w", err)
	}
	return nil
}

func selectInitialCandidate(candidates []InitialCandidate) (InitialCandidate, bool) {
	if len(candidates) == 0 {
		return InitialCandidate{}, false
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if compareInitialCandidate(candidate, best) < 0 {
			best = candidate
		}
	}
	return best, true
}

func compareInitialCandidate(left, right InitialCandidate) int {
	if order := cmp.Compare(right.HitCount, left.HitCount); order != 0 {
		return order
	}
	if order := cmp.Compare(left.FirstQueryOrder, right.FirstQueryOrder); order != 0 {
		return order
	}
	if order := cmp.Compare(left.ProviderGameID, right.ProviderGameID); order != 0 {
		return order
	}
	return cmp.Compare(left.ID, right.ID)
}

func selectInitialAssets(change *InitialDraftChange, assets []InitialAsset) {
	assets = slices.Clone(assets)
	slices.SortFunc(assets, func(left, right InitialAsset) int {
		if order := cmp.Compare(left.Ordinal, right.Ordinal); order != 0 {
			return order
		}
		return cmp.Compare(left.ID, right.ID)
	})
	for _, asset := range assets {
		switch asset.Kind {
		case "COVER":
			if change.CoverID == nil {
				change.CoverID = &asset.ID
			}
		case "BACKGROUND":
			if change.BackgroundID == nil {
				change.BackgroundID = &asset.ID
			}
		case "SCREENSHOT":
			change.Screenshots = append(change.Screenshots, asset)
		}
	}
}
