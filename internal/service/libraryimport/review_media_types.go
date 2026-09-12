package libraryimport

import "context"

type ReviewMediaReader interface {
	UploadedAssets(context.Context, string) ([]ReviewUploadedAsset, error)
	SourceMedia(context.Context, string) (ReviewSourceMedia, bool, error)
	RuntimeScreenshot(context.Context, string, string) (ReviewRuntimeScreenshot, bool, error)
}
type ReviewUploadedAsset struct {
	ID          string `json:"assetId"`
	Kind        string `json:"kind"`
	WidthPX     int64  `json:"widthPx"`
	HeightPX    int64  `json:"heightPx"`
	MediaType   string `json:"mediaType"`
	CreatedAtMS int64  `json:"createdAtMs"`
	URL         string `json:"url"`
}
type ReviewSourceMedia struct {
	SourceKind               string  `json:"sourceKind"`
	SourceRefID              string  `json:"sourceRefId"`
	ImportID                 string  `json:"-"`
	Label                    *string `json:"sourceLabel"`
	HasCover                 bool    `json:"-"`
	HasVideo                 bool    `json:"-"`
	CoverWidthPX             *int64  `json:"coverWidthPx"`
	CoverHeightPX            *int64  `json:"coverHeightPx"`
	CoverURL                 *string `json:"coverUrl"`
	VideoURL                 *string `json:"videoUrl"`
	PegasusImportID          string  `json:"pegasusImportId,omitempty"`
	EmulationStationImportID string  `json:"emulationStationImportId,omitempty"`
}
type ReviewRuntimeScreenshot struct {
	ID           string `json:"screenshotId"`
	ValidationID string `json:"validationId"`
	ProviderID   string `json:"providerId"`
	TargetID     string `json:"targetId"`
	WidthPX      int64  `json:"widthPx"`
	HeightPX     int64  `json:"heightPx"`
	CapturedAtMS int64  `json:"capturedAtMs"`
	URL          string `json:"url"`
}
