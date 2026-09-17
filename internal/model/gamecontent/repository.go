package gamecontent

import (
	"context"

	"retrom/internal/capability/content/contentcapability"
	validation "retrom/internal/model/corevalidation"
)

//nolint:interfacebloat // named commands replace former WithRead/CommitWrite callbacks
type Repository interface {
	// Snapshot reads – replace former WithRead callbacks.
	ReadInput(context.Context, string, int64) (StoredInput, error)
	ReadFiles(context.Context, string) ([]UploadedFile, error)
	ReadAdminGame(context.Context, string) (AdminGameDetail, error)

	// Named commands – replace former CommitWrite callbacks.
	CommitClaimLease(context.Context, Claim) (bool, error)
	CommitRefreshLease(context.Context, Claim, int64) (bool, error)
	CommitSchedule(context.Context, ScheduleCommand) (ScheduleResult, error)
	CommitPatchGame(context.Context, AdminGamePatchRequest) (AdminGamePatchResult, error)
	CommitDeleteGame(context.Context, DeleteGameRequest) (DeleteGameResult, error)
	CommitPublish(context.Context, PublishCommand) (PublishResult, error)
	CommitSettleFailure(context.Context, SettleFailureCommand) (SettleFailureResult, error)
}

// ScheduleCommand carries all inputs for the schedule-and-enqueue
// atomic operation. The repository resolves binding/upload/capabilities
// inside the transaction and enqueues the replacement job.
type ScheduleCommand struct {
	GameID, UploadID, ContentMode string
	ExpectedVersion, NowMS        int64
	PrincipalID, Key, Digest      string
	MultiDiscImportEnabled        bool
}

// ScheduleResult is the outcome of CommitSchedule.
type ScheduleResult struct {
	GameID   string `json:"gameId"`
	JobID    string `json:"jobId"`
	State    string `json:"state"`
	Version  int64  `json:"version"`
	Replayed bool   `json:"-"`
}

// PublishCommand carries all inputs for the atomic publication commit.
type PublishCommand struct {
	Claim    Claim
	Snapshot JobSnapshot
	Prepared PreparedReplacement
	NowMS    int64
}

// PublishResult reports post-commit side-effect triggers.
type PublishResult struct {
	SignalRelease bool
}

// SettleFailureCommand carries data for the atomic failure settlement.
type SettleFailureCommand struct {
	Claim   Claim
	Outcome Outcome
}

// SettleFailureResult reports what the settlement did.
type SettleFailureResult struct {
	Changed       bool
	Retryable     bool
	SignalRelease bool
}

// ---------------------------------------------------------------------------
// Internal scope types – used only by the repo implementation.
// ---------------------------------------------------------------------------

type ReadScope struct {
	Content Reader
	Inputs  InputReader
	BIOS    validation.Repository
	Admin   AdminGameReader
}
type WriteScope struct {
	ReadScope
	Replays            ReplayRecords
	Jobs               JobWriter
	Leases             LeaseRecords
	ContentWriter      ContentWriter
	Retirements        RetirementScope
	AdminWriter        AdminGameWriter
	GameDeletionReader DeleteGameReader
	GameDeletionWriter DeleteGameWriter
}

// AdminGameReader provides the complete detail projection used by the
// administrative game endpoint. The projection is read in one persistence
// snapshot so the game, files, assets, and variants agree with one another.
type AdminGameReader interface {
	AdminGame(context.Context, string) (AdminGameDetail, error)
}

// AdminGameWriter owns the transaction-bound state transition for an
// administrative metadata patch. SQL and audit persistence stay behind this
// port while the service applies the patch to the current snapshot.
type AdminGameWriter interface {
	LoadPatchState(context.Context, string) (AdminGamePatchState, error)
	UpdatePatch(context.Context, AdminGamePatchUpdate) (bool, error)
}
type Reader interface {
	Binding(context.Context, string) (Binding, error)
	Upload(context.Context, string) (Upload, error)
	Files(context.Context, string) ([]UploadedFile, error)
	Identity(context.Context, string) ([]IdentityFile, error)
}
type InputReader interface {
	Input(context.Context, string, int64) (StoredInput, error)
}
type StoredInput struct {
	Contents []byte
	Digest   string
}
type Upload struct {
	State, SourceType       string
	FileCount, Consumptions int64
}
type (
	IdentityFile struct{ Role, SHA256 string }
	Binding      struct {
		ManifestDigest, InstanceID, PlatformID, CoreID                                               string
		ProviderID, TargetID                                                                         string
		ContentPolicy                                                                                contentcapability.Policy
		VariantID, RPGGeneration, RPGDependencySHA256, RPGRequirementsSHA256, DependencySnapshotJSON string
		Version, PlatformVersion                                                                     int64
		DATID                                                                                        *string
	}
)

type Replay struct {
	Digest string
	Body   []byte
}
type ReplayWrite struct {
	PrincipalID, Key, Digest string
	Headers, Body            []byte
	Now, ExpiresAt           int64
}
type ReplayRecords interface {
	Load(context.Context, string, string, int64) (Replay, bool, error)
	Remember(context.Context, ReplayWrite) error
}
type ScheduleWrite struct {
	JobID, ConsumptionID, GameID, UploadID, Dedupe, InputDigest string
	Input                                                       []byte
	Now                                                         int64
}
type Claim struct {
	JobID, GameID, WorkerID, InputDigest string
	ExecutionNo, Now, Deadline           int64
}
type Outcome struct {
	Claim
	GameID, Code, ManifestDigest, VariantID string
	Retryable, Cancelled                    bool
	Now, RetiredSaveCount                   int64
}
type JobWriter interface {
	Enqueue(context.Context, ScheduleWrite) error
	Fail(context.Context, Outcome) (bool, error)
	Succeed(context.Context, Outcome) error
}
type LeaseRecords interface {
	Claim(context.Context, Claim) (bool, error)
	Refresh(context.Context, Claim, int64) (bool, error)
	Current(context.Context, Claim, int64) (bool, error)
	State(context.Context, Claim) (string, error)
}
type Publication struct {
	JobID                  string
	Snapshot               JobSnapshot
	Prepared               PreparedReplacement
	DependencySnapshotJSON []byte
	Now                    int64
}
type ContentWriter interface {
	Publish(context.Context, Publication) error
}
type RetirementImpact struct {
	SaveStateCount   int64
	CandidateBlobIDs []string
}
type ReleaseSignal interface{ Signal() }
