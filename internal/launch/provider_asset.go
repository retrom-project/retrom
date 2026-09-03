package launch

import (
	"context"
	"path"
)

// ProviderAsset identifies one immutable asset declared by the active Target.
// The caller supplies only a semantic basename; the Provider manifest remains
// the sole owner of the concrete asset path.
type ProviderAsset struct {
	ProviderID   string
	BundleSHA256 string
	Path         string
}

func (service *Service) ProviderAssetAuthorized(
	ctx context.Context,
	sessionID string,
	preview bool,
	basename string,
) (ProviderAsset, error) {
	if service.runtimeBuilder == nil || basename == "" || path.Base(basename) != basename {
		return ProviderAsset{}, ErrCredential
	}
	var providerID, targetID, bundleSHA256, state string
	var hardExpires int64
	query := `
SELECT provider_id,target_id,bundle_sha256,state,hard_expires_at_ms
FROM launch_sessions WHERE id=?`
	if preview {
		query = `
SELECT provider_id,target_id,bundle_sha256,state,hard_expires_at_ms
FROM review_preview_sessions WHERE id=?`
	}
	if err := service.database.QueryRowContext(ctx, query, sessionID).Scan(
		&providerID, &targetID, &bundleSHA256, &state, &hardExpires,
	); err != nil || state != "ACTIVE" || hardExpires <= service.now().UnixMilli() {
		return ProviderAsset{}, ErrCredential
	}
	target, exists := service.runtimeBuilder.Target(providerID, targetID)
	if !exists {
		return ProviderAsset{}, ErrCredential
	}
	assetPath := ""
	for _, candidate := range target.AssetPaths {
		if path.Base(candidate) != basename {
			continue
		}
		if assetPath != "" {
			return ProviderAsset{}, ErrCredential
		}
		assetPath = candidate
	}
	if assetPath == "" {
		return ProviderAsset{}, ErrCredential
	}
	return ProviderAsset{ProviderID: providerID, BundleSHA256: bundleSHA256, Path: assetPath}, nil
}
