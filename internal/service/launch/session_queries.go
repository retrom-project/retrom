package launch

import (
	"context"
	"fmt"
	"path"
	"time"
)

type SessionQueries struct {
	reader SessionReader
	assets TargetAssets
	policy accessPolicy
}

func NewSessionQueries(
	reader SessionReader,
	assets TargetAssets,
	now func() time.Time,
	matches MatchCapability,
) *SessionQueries {
	return &SessionQueries{reader: reader, assets: assets, policy: accessPolicy{now: now, matches: matches}}
}

func (service *SessionQueries) AuthorizeSave(ctx context.Context, id, capability string) error {
	session, found, err := service.reader.SaveSession(ctx, id)
	if err != nil {
		return fmt.Errorf("read launch save access: %w", err)
	}
	if !found || (session.State != "CREATED" && session.State != "ACTIVE") ||
		session.HardExpiresAtMS <= service.policy.now().UnixMilli() || service.policy.matches == nil ||
		!service.policy.matches(capability, session.CredentialHash) {
		return ErrCredential
	}
	return nil
}

func (service *SessionQueries) BundleFiles(
	ctx context.Context,
	ref SessionRef,
	capability, kind string,
) ([]BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, ErrCredential
	}
	record, found, err := service.reader.Bundle(ctx, ref, kind)
	if err != nil {
		return nil, fmt.Errorf("read launch bundle: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return nil, ErrCredential
	}
	if record.Files == nil {
		return []BundleFile{}, nil
	}
	return record.Files, nil
}

func (service *SessionQueries) MultiDiscTelemetryDimensions(
	ctx context.Context,
	id, capability string,
) (MultiDiscTelemetryDimensions, error) {
	record, found, err := service.reader.MultiDisc(ctx, id)
	if err != nil {
		return MultiDiscTelemetryDimensions{}, fmt.Errorf("read launch multidisc telemetry: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) ||
		record.Dimensions.DiscCount < 2 || record.Dimensions.DiscCount > 8 {
		return MultiDiscTelemetryDimensions{}, ErrCredential
	}
	return record.Dimensions, nil
}

func (service *SessionQueries) ProviderAssetAuthorized(
	ctx context.Context,
	ref SessionRef,
	basename string,
) (ProviderAsset, error) {
	if service.assets == nil || basename == "" || path.Base(basename) != basename {
		return ProviderAsset{}, ErrCredential
	}
	session, found, err := service.reader.Session(ctx, ref)
	if err != nil {
		return ProviderAsset{}, fmt.Errorf("read launch provider asset authority: %w", err)
	}
	if !found || !service.policy.active(session) {
		return ProviderAsset{}, ErrCredential
	}
	candidates, exists := service.assets.AssetPaths(session.ProviderID, session.TargetID)
	if !exists {
		return ProviderAsset{}, ErrCredential
	}
	assetPath := ""
	for _, candidate := range candidates {
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
	return ProviderAsset{ProviderID: session.ProviderID, BundleSHA256: session.BundleSHA256, Path: assetPath}, nil
}
