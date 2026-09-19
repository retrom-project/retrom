package hasheous

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	metadatamodel "retrom/internal/model/metadata"
)

func (provider *Provider) FetchAsset(
	ctx context.Context,
	asset metadatamodel.AssetReference,
) (metadatamodel.AssetData, error) {
	return provider.FetchAssetBounded(ctx, asset, metadatamodel.MaximumAssetReadBytes)
}

func (provider *Provider) FetchAssetBounded(
	parent context.Context,
	asset metadatamodel.AssetReference,
	limit int64,
) (metadatamodel.AssetData, error) {
	if !validOpaqueID(asset.ProviderAssetID) || asset.Path != "/api/v1/images/"+asset.ProviderAssetID {
		return metadatamodel.AssetData{}, metadatamodel.ErrAssetURLInvalid
	}
	if limit <= 0 {
		return metadatamodel.AssetData{}, metadatamodel.ErrAssetReadLimit
	}
	limit = min(limit, metadatamodel.MaximumAssetReadBytes)
	current := &url.URL{Scheme: "https", Host: "hasheous.org", Path: asset.Path}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	for redirect := 0; redirect <= 3; redirect++ {
		if err := context.Cause(ctx); err != nil {
			return metadatamodel.AssetData{}, errors.Join(metadatamodel.ErrAssetNetwork, err)
		}
		if err := provider.validateAssetURL(ctx, current); err != nil {
			return metadatamodel.AssetData{}, err
		}
		response, err := provider.assetResponse(ctx, current)
		if err != nil {
			return metadatamodel.AssetData{}, err
		}
		if response.StatusCode >= 300 && response.StatusCode <= 399 {
			current, err = assetRedirect(response, current, redirect)
			if err != nil {
				return metadatamodel.AssetData{}, err
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			return metadatamodel.AssetData{}, errors.Join(metadatamodel.ErrAssetHTTPStatus, response.Body.Close())
		}
		return readAssetResponse(ctx, response, limit)
	}
	return metadatamodel.AssetData{}, metadatamodel.ErrAssetRedirectLimit
}

func (provider *Provider) assetResponse(ctx context.Context, target *url.URL) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, errors.Join(metadatamodel.ErrAssetURLInvalid, err)
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return nil, errors.Join(metadatamodel.ErrAssetNetwork, err, context.Cause(ctx))
	}
	return response, nil
}

func assetRedirect(response *http.Response, current *url.URL, count int) (*url.URL, error) {
	location := response.Header.Get("Location")
	if err := response.Body.Close(); err != nil {
		return nil, errors.Join(metadatamodel.ErrAssetNetwork, err)
	}
	if count == 3 {
		return nil, metadatamodel.ErrAssetRedirectLimit
	}
	next, err := current.Parse(location)
	if err != nil {
		return nil, errors.Join(metadatamodel.ErrAssetURLInvalid, err)
	}
	return next, nil
}

func (provider *Provider) validateAssetURL(ctx context.Context, target *url.URL) error {
	if target.Scheme != "https" || target.Hostname() != "hasheous.org" ||
		(target.Port() != "" && target.Port() != "443") || target.RawQuery != "" || target.Fragment != "" {
		return metadatamodel.ErrAssetURLRejected
	}
	addresses, err := provider.resolver.LookupIPAddr(ctx, target.Hostname())
	if err != nil {
		return errors.Join(metadatamodel.ErrAssetDNSFailed, fmt.Errorf("resolve media source: %w", err))
	}
	if len(addresses) == 0 {
		return metadatamodel.ErrAssetDNSFailed
	}
	for _, address := range addresses {
		if unsafeIP(address.IP) {
			return metadatamodel.ErrAssetIPRejected
		}
	}
	return nil
}

func unsafeIP(ip net.IP) bool {
	return ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}
