package launch

import (
	"context"
	"fmt"
	"path"
	"time"

	model "retrom/internal/model/launch"
)

type SessionQueries struct {
	reader model.SessionReader
	assets model.TargetAssets
	policy accessPolicy
}

func NewSessionQueries(
	reader model.SessionReader,
	assets model.TargetAssets,
	now func() time.Time,
	matches model.MatchCapability,
) *SessionQueries {
	return &SessionQueries{reader: reader, assets: assets, policy: accessPolicy{now: now, matches: matches}}
}

func (service *SessionQueries) SaveAccess(ctx context.Context, id, capability string) (string, error) {
	session, found, err := service.reader.SaveSession(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read launch save access: %w", err)
	}
	if !found || (session.State != "CREATED" && session.State != "ACTIVE") ||
		session.HardExpiresAtMS <= service.policy.now().UnixMilli() || service.policy.matches == nil ||
		!service.policy.matches(capability, session.CredentialHash) {
		return "", model.ErrCredential
	}
	return session.SaveAccess, nil
}

func (service *SessionQueries) BundleFiles(
	ctx context.Context,
	ref model.SessionRef,
	capability, kind string,
) ([]model.BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, model.ErrCredential
	}
	record, found, err := service.reader.Bundle(ctx, ref, kind)
	if err != nil {
		return nil, fmt.Errorf("read launch bundle: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return nil, model.ErrCredential
	}
	if record.Files == nil {
		return []model.BundleFile{}, nil
	}
	return record.Files, nil
}

func (service *SessionQueries) MultiDiscTelemetryDimensions(
	ctx context.Context,
	id, capability string,
) (model.MultiDiscTelemetryDimensions, error) {
	record, found, err := service.reader.MultiDisc(ctx, id)
	if err != nil {
		return model.MultiDiscTelemetryDimensions{}, fmt.Errorf("read launch multidisc telemetry: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) ||
		record.Dimensions.DiscCount < 2 || record.Dimensions.DiscCount > 8 {
		return model.MultiDiscTelemetryDimensions{}, model.ErrCredential
	}
	return record.Dimensions, nil
}

func (service *SessionQueries) ProviderAssetAuthorized(
	ctx context.Context,
	ref model.SessionRef,
	basename string,
) (model.ProviderAsset, error) {
	if service.assets == nil || basename == "" || path.Base(basename) != basename {
		return model.ProviderAsset{}, model.ErrCredential
	}
	session, found, err := service.reader.Session(ctx, ref)
	if err != nil {
		return model.ProviderAsset{}, fmt.Errorf("read launch provider asset authority: %w", err)
	}
	if !found || !service.policy.active(session) {
		return model.ProviderAsset{}, model.ErrCredential
	}
	candidates, exists := service.assets.AssetPaths(session.ProviderID, session.TargetID)
	if !exists {
		return model.ProviderAsset{}, model.ErrCredential
	}
	assetPath := ""
	for _, candidate := range candidates {
		if path.Base(candidate) != basename {
			continue
		}
		if assetPath != "" {
			return model.ProviderAsset{}, model.ErrCredential
		}
		assetPath = candidate
	}
	if assetPath == "" {
		return model.ProviderAsset{}, model.ErrCredential
	}
	return model.ProviderAsset{ProviderID: session.ProviderID, BundleSHA256: session.BundleSHA256, Path: assetPath}, nil
}
