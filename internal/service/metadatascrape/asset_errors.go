package metadatascrape

import (
	"errors"

	"retrom/internal/adapter/metadata/hasheous"
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

func mediaRetryable(cause error) bool {
	for _, permanent := range []error{
		ErrMediaInput, ErrAssetStateConflict,
		hasheous.ErrAssetURLInvalid, hasheous.ErrAssetURLRejected, hasheous.ErrAssetIPRejected,
		hasheous.ErrAssetRedirectLimit, hasheous.ErrAssetTooLarge, hasheous.ErrAssetReadLimit,
		hasheous.ErrAssetMediaTypeInvalid, hasheous.ErrAssetMediaTypeMismatch,
		hasheous.ErrAssetDecodeFailed, hasheous.ErrAssetPixelLimit,
	} {
		if errors.Is(cause, permanent) {
			return false
		}
	}
	return true
}
