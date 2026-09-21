package gamecontent

import (
	"context"

	"retrom/internal/contentcapability"
	validation "retrom/internal/service/corevalidation"
)

type Repository interface {
	WithRead(context.Context, func(ReadScope) error) error
	WithWrite(context.Context, func(WriteScope) error) error
}
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
