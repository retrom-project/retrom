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

func mediaRetryable(code string) bool {
	switch code {
	case "MEDIA_INPUT_INVALID", "MEDIA_ASSET_STATE_INVALID", "ASSET_RUN_BUDGET_EXCEEDED",
		"ASSET_URL_INVALID", "ASSET_URL_REJECTED", "ASSET_IP_REJECTED",
		"ASSET_REDIRECT_LIMIT", "ASSET_TOO_LARGE",
		"ASSET_MEDIA_TYPE_INVALID", "ASSET_MEDIA_TYPE_MISMATCH",
		"ASSET_DECODE_FAILED", "ASSET_PIXEL_LIMIT":
		return false
	default:
		return true
	}
}
