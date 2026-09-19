package gamemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	model "retrom/internal/model/gamemetadata"
)

var candidateFieldNames = map[string]struct{}{
	"title": {}, "description": {}, "developer": {}, "publisher": {},
	"genre": {}, "players": {}, "releaseYear": {},
}

func ValidCandidateFields(fields []string) bool {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := candidateFieldNames[field]; !ok {
			return false
		}
		if _, ok := seen[field]; ok {
			return false
		}
		seen[field] = struct{}{}
	}
	return true
}

func (service *Service) ApplyCandidate(
	ctx context.Context, request model.ApplyCandidateRequest,
) (model.ApplyCandidateResult, error) {
	if request.GameID == "" || request.CandidateID == "" || request.ExpectedVersion < 1 ||
		!ValidCandidateFields(request.Fields) {
		return model.ApplyCandidateResult{}, model.ErrInvalid
	}
	now := service.now().UnixMilli()
	var result model.ApplyCandidateResult
	err := service.repository.WithCandidateApply(ctx, func(scope model.CandidateApplyScope) error {
		return service.applyCandidateInScope(ctx, scope, request, now, &result)
	})
	if err != nil {
		return model.ApplyCandidateResult{}, fmt.Errorf("apply scrape candidate: %w", err)
	}
	return result, nil
}

func (service *Service) applyCandidateInScope(
	ctx context.Context,
	scope model.CandidateApplyScope,
	request model.ApplyCandidateRequest,
	now int64,
	result *model.ApplyCandidateResult,
) error {
	snapshot, err := scope.Load(ctx, request.GameID, request.CandidateID)
	if err != nil {
		return fmt.Errorf("load scrape candidate: %w", model.ErrCandidateStale)
	}
	if snapshot.Version != request.ExpectedVersion {
		return model.ErrCandidateStale
	}
	candidate, err := decodeCandidateMetadata(snapshot.CandidateMetadataJSON)
	if err != nil {
		return err
	}
	current := applyMetadataFields(snapshot.Current, candidate, request.Fields)
	if strings.TrimSpace(current.Title) == "" {
		return model.ErrMetadataInvalid
	}
	if err := service.applyCandidateAssets(ctx, scope, request, now, result); err != nil {
		return err
	}
	changed, err := scope.UpdateGameMetadata(ctx, model.GameMetadataUpdate{
		GameID: request.GameID, CandidateID: request.CandidateID,
		ExpectedVersion: request.ExpectedVersion, Metadata: current, NowMS: now,
	})
	if err != nil {
		return fmt.Errorf("persist candidate metadata: %w", err)
	}
	if !changed {
		return model.ErrVersionConflict
	}
	if err := scope.StageCandidates(ctx, result.ReplacedBlobIDs); err != nil {
		return fmt.Errorf("stage replaced candidate assets: %w", err)
	}
	result.Version = request.ExpectedVersion + 1
	result.UpdatedAtMS = now
	return nil
}

func (service *Service) applyCandidateAssets(
	ctx context.Context,
	scope model.CandidateApplyScope,
	request model.ApplyCandidateRequest,
	now int64,
	result *model.ApplyCandidateResult,
) error {
	selected := selectedCandidateAssets(request.SelectedAssets)
	for _, kind := range selectedAssetKinds(request.SelectedAssets) {
		blobIDs, err := scope.ReplaceGameAssets(ctx, request.GameID, kind)
		if err != nil {
			return fmt.Errorf("replace game assets: %w", model.ErrCandidateAsset)
		}
		result.ReplacedBlobIDs = append(result.ReplacedBlobIDs, blobIDs...)
	}
	if len(selected) == 0 {
		return nil
	}
	assetIDs, err := scope.CreateSelectedGameAssets(
		ctx, request.GameID, request.CandidateID, selected, now,
	)
	if err != nil {
		return fmt.Errorf("create selected game asset: %w", model.ErrCandidateAsset)
	}
	result.AssetIDs = append(result.AssetIDs, assetIDs...)
	return nil
}

func decodeCandidateMetadata(contents string) (map[string]any, error) {
	var candidate map[string]any
	if err := json.Unmarshal([]byte(contents), &candidate); err != nil {
		return nil, fmt.Errorf("decode scrape candidate metadata: %w", model.ErrCandidateMetadata)
	}
	return candidate, nil
}

func applyMetadataFields(current model.Metadata, candidate map[string]any, fields []string) model.Metadata {
	for _, field := range fields {
		switch field {
		case "title":
			current.Title, _ = candidate[field].(string)
		case "description":
			current.Description, _ = candidate[field].(string)
		case "developer":
			current.Developer, _ = candidate[field].(string)
		case "publisher":
			current.Publisher, _ = candidate[field].(string)
		case "genre":
			current.Genre, _ = candidate[field].(string)
		case "players":
			if candidate[field] == nil {
				current.Players = nil
			} else if value, ok := candidate[field].(float64); ok {
				converted := int64(value)
				current.Players = &converted
			}
		case "releaseYear":
			if candidate[field] == nil {
				current.ReleaseYear = nil
			} else if value, ok := candidate[field].(float64); ok {
				converted := int64(value)
				current.ReleaseYear = &converted
			}
		}
	}
	return current
}

func selectedCandidateAssets(selected model.SelectedAssets) []model.CandidateAssetSelection {
	assets := make([]model.CandidateAssetSelection, 0, len(selected.ScreenshotCandidateAssetIDs)+2)
	if selected.CoverCandidateAssetID != nil {
		assets = append(assets, model.CandidateAssetSelection{ID: *selected.CoverCandidateAssetID, Kind: "COVER"})
	}
	if selected.BackgroundCandidateAssetID != nil {
		assets = append(assets, model.CandidateAssetSelection{ID: *selected.BackgroundCandidateAssetID, Kind: "BACKGROUND"})
	}
	for ordinal, id := range selected.ScreenshotCandidateAssetIDs {
		assets = append(assets, model.CandidateAssetSelection{ID: id, Kind: "SCREENSHOT", Ordinal: int64(ordinal)})
	}
	return assets
}

func selectedAssetKinds(selected model.SelectedAssets) []string {
	kinds := make([]string, 0, 3)
	if selected.CoverCandidateAssetID != nil {
		kinds = append(kinds, "COVER")
	}
	if selected.BackgroundCandidateAssetID != nil {
		kinds = append(kinds, "BACKGROUND")
	}
	if len(selected.ScreenshotCandidateAssetIDs) > 0 {
		kinds = append(kinds, "SCREENSHOT")
	}
	return kinds
}
