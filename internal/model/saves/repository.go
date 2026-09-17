package saves

import (
	"context"
	"errors"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/runtime/runtimebundle"
)

type Repository interface {
	LoadLaunch(context.Context, string) (Launch, error)
	Restore(context.Context, string) (Restore, error)
	CommitManualCheckpoint(context.Context, ManualCheckpointCommand) (ManualResult, bool, error)
}

// ManualCheckpointCommand captures all pre-computed values for a checkpoint save.
type ManualCheckpointCommand struct {
	LaunchID, PrincipalID string
	IdempotencyKey        ReplayKey
	Digest                string
	ExpiresAtMS           int64
	Launch                Launch
	Payload               ManualCheckpointPayload
	Screenshot            *ManualCheckpointImage
	ScreenshotMediaType   string
	SaveStateID           string
	MetadataName          string
	MetadataDiscIndex     *int
}

type ManualCheckpointPayload struct {
	SHA256, MediaType string
	Size              int64
}

type ManualCheckpointImage struct {
	SHA256, MediaType string
	Size              int64
}

// ListRepository owns the administrator-facing save list projection. It is
// kept separate from the checkpoint write boundary so existing launch/save
// implementations can opt into the read path independently.
type ListRepository interface {
	List(context.Context, ListQuery) ([]ListItem, error)
}

// StateMutationRepository owns optimistic-concurrency mutations for saved
// checkpoints exposed by the HTTP API.
type StateMutationRepository interface {
	Rename(context.Context, RenameRequest) error
	Delete(context.Context, DeleteRequest) error
}

var (
	ErrCheckpointIncompatible = errors.New("RPG_CHECKPOINT_INCOMPATIBLE")
	ErrCheckpointInvalid      = errors.New("RPG_CHECKPOINT_INVALID")
	ErrCheckpointUnavailable  = errors.New("RPG_CHECKPOINT_UNAVAILABLE")
	ErrCredential             = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrInvalid                = errors.New("SAVE_INVALID")
	ErrRepositoryUnavailable  = errors.New("SAVE_REPOSITORY_UNAVAILABLE")
	ErrNotFound               = errors.New("SAVE_STATE_NOT_FOUND")
	ErrSequenceReused         = errors.New("SAVE_SEQUENCE_REUSED")
	ErrSyncConflict           = errors.New("SAVE_SYNC_CONFLICT")
	ErrTooLarge               = errors.New("SAVE_TOO_LARGE")
	ErrVersionConflict        = errors.New("SAVE_VERSION_CONFLICT")
)

type ListQuery struct {
	ProfileID, Query, GameID, PlatformID, PlatformInstanceID, CoreID string
	Availability                                                     string
	CursorCreatedAtMS                                                *int64
	CursorID                                                         string
	Limit                                                            int
}

type ListItem struct {
	ID, GameID, GameTitle, Name, CoreID, CoreName, GameStatus string
	PlatformID, PlatformName, InstanceID, InstanceName        string
	CompatibilityStatus                                       string
	Version, CreatedAtMS, ActiveDurationMS, SizeBytes         int64
	HasScreenshot                                             bool
	DiscIndex, LastSyncedAtMS                                 *int64
}

type RenameRequest struct {
	SaveStateID, ProfileID, Name string
	ExpectedVersion, UpdatedAtMS int64
}

type DeleteRequest struct {
	SaveStateID, ProfileID       string
	ExpectedVersion, UpdatedAtMS int64
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
	LocalDraft                              bool
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
