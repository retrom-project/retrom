package launch

import (
	"context"
	"fmt"

	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

func (service *Service) ReviewPreviewContent(
	ctx context.Context,
	previewID, capability, logicalName string,
) (ContentView, error) {
	result, err := service.contentAccess().PreviewContent(ctx, previewID, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewExternal(
	ctx context.Context,
	previewID, capability, logicalName string,
) (ExternalView, error) {
	result, err := service.contentAccess().External(
		ctx,
		application.SessionRef{ID: previewID, Preview: true},
		capability,
		logicalName,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewBundleFiles(
	ctx context.Context,
	previewID, capability, kind string,
) ([]BundleFile, error) {
	result, err := service.sessionQueries().BundleFiles(
		ctx,
		application.SessionRef{ID: previewID, Preview: true},
		capability,
		kind,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func reviewPreviewCredential(now int64, capability string, hash []byte, state string, hardExpires int64) bool {
	return state == "ACTIVE" && hardExpires > now && retromruntime.MatchesCapability(capability, hash)
}
