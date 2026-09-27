package libraryimport

import (
	"context"
	"errors"
	"io"

	"retrom/internal/filestore"
)

var (
	ErrReviewCoverUploadInvalid      = errors.New("REVIEW_COVER_UPLOAD_INVALID")
	ErrReviewCoverStorageUnavailable = errors.New("REVIEW_COVER_FILE_STORAGE_UNAVAILABLE")
	ErrReviewCoverImageInvalid       = errors.New("REVIEW_COVER_IMAGE_INVALID")
	ErrReviewCoverVersion            = errors.New("REVIEW_COVER_VERSION_CONFLICT")
	ErrReviewCoverConsumed           = errors.New("REVIEW_COVER_UPLOAD_CONSUMED")
	ErrReviewCoverIntegrity          = errors.New("REVIEW_COVER_OWNERSHIP_INVALID")
)

type ReviewCoverRequest struct {
	ItemID, UploadFileID, Kind string
	ExpectedVersion            int64
}
type ReviewCoverSource struct {
	FileID, UploadID, FileRecord, Digest, Purpose string
	SizeBytes                                     int64
}
type ReviewCoverDraft struct {
	Version    int64
	State      string
	SourceBusy bool
}
type ReviewCoverRecord struct {
	ID, ItemID, UploadFileID, FileRecord, MediaType string
	Width, Height                                   int
	CreatedAtMS                                     int64
}
type ReviewCoverResult struct {
	AssetID     string `json:"assetId"`
	Kind        string `json:"kind"`
	Width       int    `json:"widthPx"`
	Height      int    `json:"heightPx"`
	MediaType   string `json:"mediaType"`
	Version     int64  `json:"version"`
	CreatedAtMS int64  `json:"createdAtMs"`
}
type ReviewCoverConsumption struct {
	ID, UploadID, FileID, AssetID string
	CreatedAtMS                   int64
}
type ReviewCoverExisting struct {
	Record         ReviewCoverRecord
	HasConsumption bool
}
type ReviewCoverBlobs interface {
	CopyTo(context.Context, string, string, string) (filestore.Metadata, error)
	OpenRecord(string) (io.ReadCloser, error)
}
type ReviewCoverRepository interface {
	Source(context.Context, string) (ReviewCoverSource, bool, error)
	WithWrite(context.Context, func(ReviewCoverScope) error) error
}
type ReviewCoverScope struct {
	Reader ReviewCoverReader
	Writer ReviewCoverWriter
}
type ReviewCoverReader interface {
	Source(context.Context, string) (ReviewCoverSource, bool, error)
	Draft(context.Context, string) (ReviewCoverDraft, bool, error)
	ExistingByUpload(context.Context, string) (ReviewCoverExisting, bool, error)
}
type ReviewCoverWriter interface {
	InsertAsset(context.Context, ReviewCoverRecord) error
	Consume(context.Context, ReviewCoverConsumption) error
}
