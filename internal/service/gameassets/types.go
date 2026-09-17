package gameassets

import (
	"errors"
	"fmt"
	"os"
	"time"

	model "retrom/internal/model/gameassets"

	"github.com/google/uuid"
)

var (
	ErrInvalid         = errors.New("GAME_ASSET_INVALID")
	ErrVersionConflict = model.ErrVersionConflict
	ErrAssetNotFound   = model.ErrAssetNotFound
	ErrUploadConsumed  = model.ErrUploadConsumed
)

// ValidationError carries the stable response code and message for upload
// preparation failures.
type ValidationError struct {
	Code, Message string
	Cause         error
}

func (err *ValidationError) Error() string { return err.Code }
func (err *ValidationError) Unwrap() error { return err.Cause }

type PreparedAsset struct {
	UploadID, BlobID, MediaType string
	Digest                      string
	SizeBytes                   int64
	WidthPX, HeightPX           *int64
}

type CreateRequest struct {
	GameID, UploadFileID, Kind string
	Ordinal                    int64
	ExpectedVersion, NowMS     int64
	Asset                      PreparedAsset
}

type CreateResult struct {
	AssetID, GameID, Kind string
	Ordinal               int64
	WidthPX, HeightPX     *int64
	MediaType             string
	Version, CreatedAtMS  int64
}

type DeleteRequest struct {
	GameID, Kind    string
	ExpectedVersion int64
	NowMS           int64
}

type DeleteResult struct {
	Version int64
}

// BlobReader opens an immutable CAS object for media inspection.
type BlobReader interface {
	OpenDigest(string) (*os.File, error)
}

type Service struct {
	repository model.Repository
	blobs      BlobReader
	now        func() time.Time
	newID      func() (string, error)
}

func New(repository model.Repository, blobs BlobReader, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		repository: repository,
		blobs:      blobs,
		now:        now,
		newID: func() (string, error) {
			id, err := uuid.NewV7()
			if err != nil {
				return "", fmt.Errorf("create game asset ID: %w", err)
			}
			return id.String(), nil
		},
	}
}

// WithIDFactory replaces UUID generation for deterministic application tests.
func (service *Service) WithIDFactory(factory func() (string, error)) *Service {
	if factory != nil {
		service.newID = factory
	}
	return service
}

func ValidUpload(kind string, ordinal int64) bool {
	validKind := kind == "COVER" || kind == "BACKGROUND" || kind == "SCREENSHOT" || kind == "VIDEO"
	return validKind && ordinal >= 0 && ordinal <= 31 &&
		(kind == "SCREENSHOT" || ordinal == 0)
}
