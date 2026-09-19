package metadata

import "errors"

const (
	MaximumAssetBytes     = 10 << 20
	MaximumAssetReadBytes = MaximumAssetBytes + 1
	MaximumAssetPixels    = 40_000_000
)

var (
	ErrAssetURLInvalid        = errors.New("ASSET_URL_INVALID")
	ErrAssetNetwork           = errors.New("ASSET_NETWORK_ERROR")
	ErrAssetRedirectLimit     = errors.New("ASSET_REDIRECT_LIMIT")
	ErrAssetHTTPStatus        = errors.New("ASSET_HTTP_STATUS")
	ErrAssetTooLarge          = errors.New("ASSET_TOO_LARGE")
	ErrAssetURLRejected       = errors.New("ASSET_URL_REJECTED")
	ErrAssetDNSFailed         = errors.New("ASSET_DNS_FAILED")
	ErrAssetIPRejected        = errors.New("ASSET_IP_REJECTED")
	ErrAssetMediaTypeInvalid  = errors.New("ASSET_MEDIA_TYPE_INVALID")
	ErrAssetMediaTypeMismatch = errors.New("ASSET_MEDIA_TYPE_MISMATCH")
	ErrAssetDecodeFailed      = errors.New("ASSET_DECODE_FAILED")
	ErrAssetPixelLimit        = errors.New("ASSET_PIXEL_LIMIT")
	ErrAssetReadLimit         = errors.New("ASSET_READ_LIMIT_EXCEEDED")
)
