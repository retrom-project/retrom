package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/libraryimport"

	taggingmodel "retrom/internal/model/tagging"
	"retrom/internal/service/metadatascrape"
	taggingservice "retrom/internal/service/tagging"
)

type ReviewDetails struct{ repository model.ReviewDetailRepository }

func NewReviewDetails(repository model.ReviewDetailRepository) *ReviewDetails {
	return &ReviewDetails{repository: repository}
}

func (service *ReviewDetails) Get(ctx context.Context, itemID string) (model.ReviewDetail, error) {
	var result model.ReviewDetail
	err := service.repository.WithRead(ctx, func(scope model.ReviewReadScope) error {
		head, err := scope.Drafts.Head(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read review headline: %w", err)
		}
		result, err = readReviewDetail(ctx, scope, head)
		return err
	})
	if err != nil {
		return model.ReviewDetail{}, fmt.Errorf("read review detail: %w", err)
	}
	return result, nil
}

func readReviewDetail(ctx context.Context, scope model.ReviewReadScope, head model.ReviewHead) (model.ReviewDetail, error) {
	result, err := projectReviewHead(head)
	if err != nil {
		return model.ReviewDetail{}, err
	}
	evidence, err := metadatascrape.NewEvidenceQueries(scope.Metadata).Review(ctx, head.ItemID)
	if err != nil {
		return model.ReviewDetail{}, fmt.Errorf("read metadata evidence: %w", err)
	}
	result.Candidates, result.ScrapeRuns = evidence.Candidates, evidence.Runs
	if err := readReviewMedia(ctx, scope, head, &result); err != nil {
		return model.ReviewDetail{}, err
	}
	if err := readReviewSelections(ctx, scope, head, &result); err != nil {
		return model.ReviewDetail{}, err
	}
	if err := readReviewContent(ctx, scope, head, &result); err != nil {
		return model.ReviewDetail{}, err
	}
	if err := readReviewValidation(ctx, scope, head, &result); err != nil {
		return model.ReviewDetail{}, err
	}
	result.Tags, err = taggingservice.ReviewDraftReferencesInScope(ctx, scope.Tags, head.DraftID)
	if err != nil {
		return model.ReviewDetail{}, fmt.Errorf("read review tags: %w", err)
	}
	if result.Tags == nil {
		result.Tags = []taggingmodel.Reference{}
	}
	return result, nil
}

func projectReviewHead(head model.ReviewHead) (model.ReviewDetail, error) {
	metadata, err := reviewDocument(head.MetadataJSON)
	if err != nil {
		return model.ReviewDetail{}, err
	}
	manifest, err := reviewDocument(head.SourceManifestJSON)
	if err != nil {
		return model.ReviewDetail{}, err
	}
	if len(manifest) > 0 && manifest[0] == '[' {
		manifest, err = json.Marshal(struct {
			Files json.RawMessage `json:"files"`
		}{manifest})
		if err != nil {
			return model.ReviewDetail{}, fmt.Errorf("encode review source manifest: %w", err)
		}
	}
	return model.ReviewDetail{
		ItemID: head.ItemID, ImportJobID: head.ImportJobID, Version: head.Version, UpdatedAtMS: head.UpdatedAtMS,
		SnapshotID: head.SnapshotID, PlatformInstance: head.PlatformInstance, Metadata: metadata, SourceManifest: manifest,
		SelectedCandidateID: head.SelectedCandidateID, DefaultDOSEntry: head.DefaultDOSEntry,
		SelectedAssets: model.ReviewSelectedAssets{
			CoverID:         head.CoverID,
			UploadedCoverID: head.UploadedCoverID,
			BackgroundID:    head.BackgroundID,
		},
	}, nil
}

func reviewDocument(value string) (json.RawMessage, error) {
	var result json.RawMessage
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil, fmt.Errorf("decode review document: %w", err)
	}
	return result, nil
}

func readReviewSelections(ctx context.Context, scope model.ReviewReadScope, head model.ReviewHead, result *model.ReviewDetail) error {
	var err error
	result.SelectedAssets.ScreenshotIDs, err = scope.Drafts.ScreenshotIDs(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read review screenshot selection: %w", err)
	}
	result.DOSEntries, err = scope.Drafts.DOSEntries(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read review DOS entries: %w", err)
	}
	return nil
}

func readReviewContent(ctx context.Context, scope model.ReviewReadScope, head model.ReviewHead, result *model.ReviewDetail) error {
	var err error
	result.DuplicateGames, result.ContentIdentityDigest, err = model.NewContentDuplicates(scope.Duplicates).Inspect(ctx,
		model.ContentSnapshot{ID: head.SnapshotID, Kind: head.ContentKind}, head.PlatformID)
	if err != nil {
		return fmt.Errorf("inspect content duplicates: %w", err)
	}
	dependencies := NewReviewDependencies(scope.Dependencies)
	dependencyHead := model.ReviewDependencyHead{
		SnapshotID: head.SnapshotID, ContentKind: head.ContentKind, PlatformID: head.PlatformID,
		ValidationStatus:  head.ValidationStatus,
		CompatibilityCode: head.CompatibilityCode,
		DependencyJSON:    head.DependencyJSON,
	}
	arcade, found, err := dependencies.Arcade(ctx, head.ItemID, dependencyHead)
	if err != nil {
		return err
	}
	if found {
		result.ArcadeDependencies = &arcade
	}
	discs, found, err := dependencies.MultiDisc(ctx, head.ItemID, dependencyHead)
	if err != nil {
		return err
	}
	if found {
		result.MultiDisc = &discs
	}
	return nil
}
