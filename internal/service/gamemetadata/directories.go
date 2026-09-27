package gamemetadata

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/filestore"

	"github.com/google/uuid"
)

func gameMediaDirectory(gameID, assetID string) string {
	return filestore.GameDirectory(gameID) + "/media/" + assetID
}

func (service *Service) prepareAssets(ctx context.Context,
	request ApplyCandidateRequest,
) ([]CandidateAssetSelection, error) {
	selected := selectedCandidateAssets(request.SelectedAssets)
	if len(selected) == 0 {
		return nil, nil
	}
	selected, err := service.repository.SelectedFiles(ctx, request.CandidateID, selected)
	if err != nil {
		return nil, fmt.Errorf("prepare assets: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			for _, asset := range selected {
				if asset.AssetID != "" {
					cleanup.Error("discard incomplete media",
						service.files.RemovePath(context.WithoutCancel(ctx), gameMediaDirectory(request.GameID, asset.AssetID)))
				}
			}
		}
	}()
	for i := range selected {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("prepare assets: %w", err)
		}
		selected[i].AssetID = id.String()
		file, err := service.files.CopyTo(ctx, selected[i].SourceFile,
			gameMediaDirectory(request.GameID, id.String()), "asset")
		if err != nil {
			return nil, fmt.Errorf("prepare assets: %w", err)
		}
		selected[i].File = file.Record
	}
	complete = true
	return selected, nil
}
