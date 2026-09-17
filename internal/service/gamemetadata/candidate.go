package gamemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	model "retrom/internal/model/gamemetadata"
)

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

func selectedCandidateAssets(selected model.SelectedAssets) []model.CandidateAssetSelection {
	assets := make(
		[]model.CandidateAssetSelection, 0,
		len(selected.ScreenshotCandidateAssetIDs)+2,
	)
	if selected.CoverCandidateAssetID != nil {
		assets = append(assets, model.CandidateAssetSelection{
			ID: *selected.CoverCandidateAssetID, Kind: "COVER",
		})
	}
	if selected.BackgroundCandidateAssetID != nil {
		assets = append(assets, model.CandidateAssetSelection{
			ID: *selected.BackgroundCandidateAssetID, Kind: "BACKGROUND",
		})
	}
	for ordinal, id := range selected.ScreenshotCandidateAssetIDs {
		assets = append(assets, model.CandidateAssetSelection{
			ID: id, Kind: "SCREENSHOT", Ordinal: int64(ordinal),
		})
	}
	return assets
}

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
	snapshot, err := service.repository.LoadCandidateApplySnapshot(ctx, request.GameID, request.CandidateID)
	if err != nil {
		return model.ApplyCandidateResult{},
			fmt.Errorf("load scrape candidate: %w", model.ErrCandidateStale)
	}
	if snapshot.Version != request.ExpectedVersion {
		return model.ApplyCandidateResult{}, model.ErrCandidateStale
	}
	candidate, err := decodeCandidateMetadata(snapshot.CandidateMetadataJSON)
	if err != nil {
		return model.ApplyCandidateResult{}, err
	}
	current := applyMetadataFields(snapshot.Current, candidate, request.Fields)
	if strings.TrimSpace(current.Title) == "" {
		return model.ApplyCandidateResult{}, model.ErrMetadataInvalid
	}
	selectedAssets := selectedCandidateAssets(request.SelectedAssets)
	kinds := selectedAssetKinds(request.SelectedAssets)
	commitResult, err := service.repository.CommitCandidateApply(ctx, model.CandidateApplyCommand{
		GameID:          request.GameID,
		CandidateID:     request.CandidateID,
		ExpectedVersion: request.ExpectedVersion,
		NowMS:           now,
		Metadata:        current,
		SelectedAssets:  selectedAssets,
		SelectedKinds:   kinds,
	})
	if err != nil {
		return model.ApplyCandidateResult{}, fmt.Errorf("apply scrape candidate: %w", err)
	}
	return model.ApplyCandidateResult{
		Version:         request.ExpectedVersion + 1,
		UpdatedAtMS:     now,
		ReplacedBlobIDs: commitResult.ReplacedBlobIDs,
		AssetIDs:        commitResult.AssetIDs,
	}, nil
}

func decodeCandidateMetadata(contents string) (map[string]any, error) {
	var candidate map[string]any
	if err := json.Unmarshal([]byte(contents), &candidate); err != nil {
		return nil, fmt.Errorf("decode scrape candidate metadata: %w", model.ErrCandidateMetadata)
	}
	return candidate, nil
}

func applyMetadataFields(
	current model.Metadata, candidate map[string]any, fields []string,
) model.Metadata {
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
			current.Players = nullableInt64Field(candidate, field)
		case "releaseYear":
			current.ReleaseYear = nullableInt64Field(candidate, field)
		}
	}
	return current
}

func nullableInt64Field(candidate map[string]any, key string) *int64 {
	raw := candidate[key]
	if raw == nil {
		return nil
	}
	if value, ok := raw.(float64); ok {
		converted := int64(value)
		return &converted
	}
	return nil
}
