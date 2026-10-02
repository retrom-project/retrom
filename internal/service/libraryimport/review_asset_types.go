package libraryimport

import (
	"context"
	"errors"
	"io"

	"retrom/internal/filestore"
)

var (
	ErrReviewAssetUploadInvalid      = errors.New("REVIEW_ASSET_UPLOAD_INVALID")
	ErrReviewAssetStorageUnavailable = errors.New("REVIEW_ASSET_FILE_STORAGE_UNAVAILABLE")
	ErrReviewAssetImageInvalid       = errors.New("REVIEW_ASSET_IMAGE_INVALID")
	ErrReviewAssetVersion            = errors.New("REVIEW_ASSET_VERSION_CONFLICT")
	ErrReviewAssetConsumed           = errors.New("REVIEW_ASSET_UPLOAD_CONSUMED")
	ErrReviewAssetIntegrity          = errors.New("REVIEW_ASSET_OWNERSHIP_INVALID")
)

var ErrReviewAssetVideoInvalid = errors.New("REVIEW_ASSET_VIDEO_INVALID")

type ReviewAssetRequest struct {
	ItemID, UploadFileID, Kind string
	ExpectedVersion            int64
}
type ReviewAssetSource struct {
	FileID, UploadID, FileRecord, Digest, Purpose string
	SizeBytes                                     int64
}
type ReviewAssetDraft struct {
	Version    int64
	State      string
	SourceBusy bool
}
type ReviewAssetRecord struct {
	ID, ItemID, UploadFileID, FileRecord, MediaType, Kind string
	Width, Height                                         *int
	CreatedAtMS                                           int64
}
type ReviewAssetResult struct {
	AssetID     string `json:"assetId"`
	Kind        string `json:"kind"`
	Width       *int   `json:"widthPx"`
	Height      *int   `json:"heightPx"`
	MediaType   string `json:"mediaType"`
	Version     int64  `json:"version"`
	CreatedAtMS int64  `json:"createdAtMs"`
}
type ReviewAssetConsumption struct {
	ID, UploadID, FileID, AssetID string
	CreatedAtMS                   int64
}
type ReviewAssetExisting struct {
	Record         ReviewAssetRecord
	HasConsumption bool
}
type ReviewAssetBlobs interface {
	CopyTo(context.Context, string, string, string) (filestore.Metadata, error)
	OpenRecord(string) (io.ReadCloser, error)
}
type ReviewAssetRepository interface {
	Source(context.Context, string) (ReviewAssetSource, bool, error)
	WithWrite(context.Context, func(ReviewAssetScope) error) error
}
type ReviewAssetScope struct {
	Reader ReviewAssetReader
	Writer ReviewAssetWriter
}
type ReviewAssetReader interface {
	Source(context.Context, string) (ReviewAssetSource, bool, error)
	Draft(context.Context, string) (ReviewAssetDraft, bool, error)
	ExistingByUpload(context.Context, string) (ReviewAssetExisting, bool, error)
}
type ReviewAssetWriter interface {
	InsertAsset(context.Context, ReviewAssetRecord) error
	Consume(context.Context, ReviewAssetConsumption) error
}
