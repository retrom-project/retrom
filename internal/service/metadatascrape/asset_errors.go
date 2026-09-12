package metadatascrape

import (
	"errors"

	"retrom/internal/hasheous"
)

func stableAssetError(err error) string {
	for _, known := range []error{
		hasheous.ErrAssetURLInvalid,
		hasheous.ErrAssetNetwork,
		hasheous.ErrAssetRedirectLimit,
		hasheous.ErrAssetHTTPStatus,
		hasheous.ErrAssetTooLarge,
		hasheous.ErrAssetURLRejected,
		hasheous.ErrAssetDNSFailed,
		hasheous.ErrAssetIPRejected,
		hasheous.ErrAssetMediaTypeInvalid,
		hasheous.ErrAssetMediaTypeMismatch,
		hasheous.ErrAssetDecodeFailed,
		hasheous.ErrAssetPixelLimit,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "ASSET_FETCH_FAILED"
}
