package cleanupjobs

import (
	"context"
	"time"
)

var ErrEffectConflict = Failure("OWNER_CLEANUP_SCOPE_VERSION_MISMATCH", nil)

type EffectOwner struct {
	Owner                         Owner
	Found                         bool
	ParentID, ExistingGameID      string
	MetadataSource, ContentSource EffectSource
	GameManifestDigest            string
	Consumption                   EffectConsumption
}

type EffectSource struct {
	Kind string
}

type EffectConsumption struct {
	ID, SessionID, FileID, ConsumerType, ConsumerID string
	Version                                         int64
	Released                                        WorkTime
}

type EffectPayload struct {
	Consumptions []EffectConsumption
}

type EffectUpload struct {
	ID, SessionID, FileRecord, State, SessionState string
	SessionVersion, ActiveConsumptions             int64
}

type EffectOwnerChange struct {
	Before   EffectOwner
	After    Owner
	Released bool
	NowMS    int64
}

type EffectConsumptionChange struct {
	Before EffectConsumption
	Reason Reason
	NowMS  int64
}

type EffectReader interface {
	Owner(context.Context, Scope) (EffectOwner, error)
	Payload(context.Context, Scope) (EffectPayload, error)
	Links(context.Context, Scope) ([]Scope, error)
	Remaining(context.Context, Scope) (int64, error)
	Mutations(context.Context, Scope) (int64, error)
}

type EffectWriter interface {
	ChangeOwner(context.Context, EffectOwnerChange) error
	Clear(context.Context, EffectOwner, int64) error
	Consume(context.Context, EffectConsumptionChange) error
}

type EffectUploads interface {
	Candidates(context.Context, string, string, int) ([]EffectUpload, error)
	Purge(context.Context, EffectUpload, int64) error
}

type EffectScope struct {
	Read    EffectReader
	Write   EffectWriter
	Uploads EffectUploads
	Worker  WorkerScope
}

type EffectRepository interface {
	WithEffects(context.Context, func(EffectScope) error) error
	ActiveMutations(context.Context, Scope) (int64, error)
}

type EffectWaiter interface {
	Wait(context.Context, time.Duration) error
}

// EffectBinding connects one fixed domain handler to its transaction repository.
type EffectBinding struct {
	Repository EffectRepository
	Prepare    func(context.Context, Scope) error
	Apply      func(context.Context, EffectScope, Execution, int64) (bool, error)
}

type ReleaseEffects struct {
	binding   EffectBinding
	authority EffectAuthority
	now       func() time.Time
}

func NewReleaseEffects(binding EffectBinding, authority EffectAuthority, now func() time.Time) *ReleaseEffects {
	return &ReleaseEffects{binding: binding, authority: authority, now: now}
}
