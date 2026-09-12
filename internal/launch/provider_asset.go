package launch

import (
	"context"
	"fmt"

	application "retrom/internal/service/launch"
)

// ProviderAsset identifies one immutable asset declared by the active Target.
type ProviderAsset = application.ProviderAsset

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
		ctx,
		application.SessionRef{ID: sessionID, Preview: preview},
		basename,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}
