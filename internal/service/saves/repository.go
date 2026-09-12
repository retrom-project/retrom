package saves

import (
	"context"

	"retrom/internal/blobstore"
	"retrom/internal/runtimebundle"
)

type Repository interface {
	LoadLaunch(context.Context, string) (Launch, error)
	Restore(context.Context, string) (Restore, error)
	WithWrite(context.Context, func(WriteScope) error) error
}
type WriteScope struct {
	Launches    LaunchReader
	Idempotency IdempotencyRecords
	Blobs       BlobRecords
	Checkpoints CheckpointRecords
	GameSaves   GameSaveRecords
}
type LaunchReader interface {
	LoadLaunch(context.Context, string) (Launch, error)
}
type IdempotencyRecords interface {
	Replay(context.Context, ReplayKey) (Replay, bool, error)
	Remember(context.Context, ReplayWrite) error
}
type BlobRecords interface {
	Ensure(context.Context, blobstore.Metadata, string, int64) (string, error)
}
type CheckpointRecords interface {
	Duration(context.Context, string) (Duration, error)
	CreateSave(context.Context, SaveCreation) error
	ReplacePreview(context.Context, PreviewWrite) error
}
type GameSaveRecords interface {
	Binding(context.Context, string) (GameSaveBinding, bool, error)
	Saved(context.Context, string) (StoredSave, bool, error)
	UpdateSave(context.Context, SaveUpdate) error
	MarkSynced(context.Context, string, string, int64) error
	Bind(context.Context, string, string, int64) error
}

type Launch struct {
	PrincipalID, ProfileID, Purpose, GameID string
	ProviderID, TargetID                    string
	DOSEntry                                *string
	CredentialHash                          []byte
	State                                   string
	HardExpiresAtMS                         int64
	Checkpoint                              runtimebundle.Checkpoint
	ContentFormat                           string
	DiscCount, InitialDiscIndex             int
	GameStatus, ItemState, PayloadState     string
	HasGameSaveBinding                      bool
	localDraft                              bool
}
type Restore struct {
	Checkpoint     runtimebundle.Checkpoint
	Format, Digest string
	Size           int64
}
type ReplayKey struct {
	PrincipalID, Key string
	AtMS             int64
}
type Replay struct {
	Digest string
	Body   []byte
}
type ReplayWrite struct {
	ReplayKey
	Replay
	ExpiresAtMS int64
}
type (
	Duration     struct{ ActiveMS, InitialMS int64 }
	SaveCreation struct {
		LaunchID, ProfileID, GameID, PayloadID string
		DOSEntry, ScreenshotID                 *string
		Payload                                blobstore.Metadata
		Result                                 ManualResult
	}
)

type PreviewWrite struct {
	PreviewID, PayloadID, Format string
	AtMS                         int64
}
type GameSaveBinding struct {
	ID              *string
	ExpectedVersion int64
}
type StoredSave struct {
	Result                            ManualResult
	ProfileID, GameID, Digest, Format string
	DataVersion                       int64
	DeletedAtMS                       *int64
	ScreenshotID                      *string
}
type SaveUpdate struct {
	SaveID, LaunchID, PayloadID, ScreenshotID   string
	Payload                                     blobstore.Metadata
	ExpectedDataVersion, AtMS, ActiveDurationMS int64
}
