package gameassets

import "context"

// Repository owns the transaction boundary for game asset commands.
type Repository interface {
	Upload(context.Context, string) (UploadedFile, bool, error)
	WithWrite(context.Context, func(WriteScope) error) error
}

// WriteScope contains the storage operations that must commit atomically with
// a game asset replacement.
//
//nolint:interfacebloat // the asset mutation is one atomic transaction scope
type WriteScope interface {
	GameVersion(context.Context, string) (int64, error)
	AssetExists(context.Context, string, string) (bool, error)
	RemoveSlot(context.Context, string, string, int64) ([]string, error)
	Create(context.Context, AssetRecord) error
	ConsumeUpload(context.Context, ConsumptionRecord) error
	UpdateGame(context.Context, string, int64, int64) (bool, error)
	StageCandidates(context.Context, []string) error
	ScheduleConsumption(context.Context, string, int64) error
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
