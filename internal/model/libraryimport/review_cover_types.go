package libraryimport

import (
	"context"
	"errors"
	"io"
)

var (
	ErrReviewCoverUploadInvalid  = errors.New("REVIEW_COVER_UPLOAD_INVALID")
	ErrReviewCoverCASUnavailable = errors.New("REVIEW_COVER_CAS_UNAVAILABLE")
	ErrReviewCoverImageInvalid   = errors.New("REVIEW_COVER_IMAGE_INVALID")
	ErrReviewCoverVersion        = errors.New("REVIEW_COVER_VERSION_CONFLICT")
	ErrReviewCoverConsumed       = errors.New("REVIEW_COVER_UPLOAD_CONSUMED")
	ErrReviewCoverIntegrity      = errors.New("REVIEW_COVER_OWNERSHIP_INVALID")
)

type ReviewCoverRequest struct {
	ItemID, UploadFileID, Kind string
	ExpectedVersion            int64
}
type ReviewCoverSource struct {
	FileID, UploadID, BlobID, Digest, Purpose string
	SizeBytes                                 int64
}
type ReviewCoverDraft struct {
	Version                           int64
	State, HandoffKind                string
	EmulationStationReady, SourceBusy bool
}
type ReviewCoverRecord struct {
	ID, ItemID, UploadFileID, BlobID, MediaType string
	Width, Height                               int
	CreatedAtMS                                 int64
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
	OpenDigest(string) (io.ReadCloser, error)
}
type ReviewCoverRepository interface {
	Source(context.Context, string) (ReviewCoverSource, bool, error)
	CommitWrite(context.Context, func(ReviewCoverScope) error) error
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
