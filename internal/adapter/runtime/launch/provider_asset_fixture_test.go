package launch

import (
	"context"
	"fmt"

	launchmodel "retrom/internal/model/launch"
)

func (service *Service) ProviderAssetAuthorized(
	ctx context.Context,
	sessionID string,
	preview bool,
	basename string,
) (ProviderAsset, error) {
	if service.runtimeBuilder == nil {
		return ProviderAsset{}, ErrCredential
	}
	result, err := service.sessionQueries().ProviderAssetAuthorized(
		ctx, launchmodel.SessionRef{ID: sessionID, Preview: preview}, basename,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}
