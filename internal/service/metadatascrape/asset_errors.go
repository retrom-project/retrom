package metadatascrape

import (
	"errors"

	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

func stableAssetError(err error) string {
	for _, known := range []error{
		metadatamodel.ErrAssetURLInvalid,
		metadatamodel.ErrAssetNetwork,
		metadatamodel.ErrAssetRedirectLimit,
		metadatamodel.ErrAssetHTTPStatus,
		metadatamodel.ErrAssetTooLarge,
		metadatamodel.ErrAssetURLRejected,
		metadatamodel.ErrAssetDNSFailed,
		metadatamodel.ErrAssetIPRejected,
		metadatamodel.ErrAssetMediaTypeInvalid,
		metadatamodel.ErrAssetMediaTypeMismatch,
		metadatamodel.ErrAssetDecodeFailed,
		metadatamodel.ErrAssetPixelLimit,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "ASSET_FETCH_FAILED"
}

func mediaRetryable(cause error) bool {
	for _, permanent := range []error{
		metadatascrapemodel.ErrMediaInput, metadatascrapemodel.ErrAssetStateConflict,
		metadatamodel.ErrAssetURLInvalid, metadatamodel.ErrAssetURLRejected, metadatamodel.ErrAssetIPRejected,
		metadatamodel.ErrAssetRedirectLimit, metadatamodel.ErrAssetTooLarge, metadatamodel.ErrAssetReadLimit,
		metadatamodel.ErrAssetMediaTypeInvalid, metadatamodel.ErrAssetMediaTypeMismatch,
		metadatamodel.ErrAssetDecodeFailed, metadatamodel.ErrAssetPixelLimit,
	} {
		if errors.Is(cause, permanent) {
			return false
		}
	}
	return true
}
