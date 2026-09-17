package gameassets

import (
	"context"
	"errors"
)

var (
	ErrVersionConflict = errors.New("GAME_ASSET_VERSION_CONFLICT")
	ErrAssetNotFound   = errors.New("GAME_ASSET_NOT_FOUND")
	ErrUploadConsumed  = errors.New("GAME_ASSET_UPLOAD_CONSUMED")
)

// CreateCommand captures all inputs for an atomic asset creation.
type CreateCommand struct {
	GameID, UploadFileID   string
	ExpectedVersion, NowMS int64
	Kind                   string
	Ordinal                int64
	AssetID, ConsumptionID string
	Asset                  PreparedAsset
}

// DeleteCommand captures all inputs for an atomic asset deletion.
type DeleteCommand struct {
	GameID          string
	Kind            string
	ExpectedVersion int64
	NowMS           int64
}

// DeleteResult carries the new version after deletion.
type DeleteResult struct {
	Version int64
}

// PreparedAsset holds CAS-validated upload metadata.
type PreparedAsset struct {
	UploadID, BlobID, Digest string
	SizeBytes                int64
	MediaType                string
	WidthPX, HeightPX        *int64
}

// Repository owns the transaction boundary for game asset commands.
type Repository interface {
	Upload(context.Context, string) (UploadedFile, bool, error)
	CommitCreate(context.Context, CreateCommand) error
	CommitDelete(context.Context, DeleteCommand) (DeleteResult, error)
}

type UploadedFile struct {
	UploadID, BlobID, Digest string
	SizeBytes                int64
}

type AssetRecord struct {
	ID, GameID, BlobID, Kind string
	Ordinal                  int64
	WidthPX, HeightPX        *int64
	MediaType                string
	CreatedAtMS              int64
}

type ConsumptionRecord struct {
	ID, UploadID, UploadFileID, ConsumerID string
	CreatedAtMS                            int64
}
