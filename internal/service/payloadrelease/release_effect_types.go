package payloadrelease

import (
	"context"
	"time"
)

var ErrEffectConflict = effectFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH", nil)

type EffectOwner struct {
	Owner                         Owner
	Found, DuplicateMatch         bool
	ParentID, ExistingGameID      string
	MetadataSource, ContentSource EffectSource
	Consumption                   EffectConsumption
}

type EffectSource struct {
	Kind, ID string
}

type EffectConsumption struct {
	ID, SessionID, FileID, ConsumerType, ConsumerID string
	Version                                         int64
	Released                                        WorkTime
}

type EffectPayload struct {
	BlobIDs      []string
	Consumptions []EffectConsumption
}

type EffectUpload struct {
	ID, SessionID, BlobID, State, SessionState           string
	SessionVersion, ActiveConsumptions, DomainReferences int64
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

type EffectReferenceGroup string

const (
	EffectGameRuntime    EffectReferenceGroup = "GAME_RUNTIME"
	EffectGameEvidence   EffectReferenceGroup = "GAME_EVIDENCE"
	EffectGameFiles      EffectReferenceGroup = "GAME_FILES"
	EffectImportReview   EffectReferenceGroup = "IMPORT_REVIEW"
	EffectImportEvidence EffectReferenceGroup = "IMPORT_EVIDENCE"
	EffectImportFiles    EffectReferenceGroup = "IMPORT_FILES"
	EffectSourceFiles    EffectReferenceGroup = "SOURCE_FILES"
	EffectSourceAssets   EffectReferenceGroup = "SOURCE_ASSETS"
)

type EffectRemoval struct {
	Before EffectOwner
	Group  EffectReferenceGroup
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
	Remove(context.Context, EffectRemoval) error
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
	GC      GCScope
}

type EffectRepository interface {
	WithEffects(context.Context, func(EffectScope) error) error
	ActiveMutations(context.Context, Scope) (int64, error)
}

type EffectWaiter interface {
	Wait(context.Context, time.Duration) error
}

type ReleaseEffects struct {
	repository EffectRepository
	authority  EffectAuthority
	gc         GCStager
	waiter     EffectWaiter
	now        func() time.Time
}

func NewReleaseEffects(
	repository EffectRepository,
	authority EffectAuthority,
	gc GCStager,
	waiter EffectWaiter,
	now func() time.Time,
) *ReleaseEffects {
	return &ReleaseEffects{repository: repository, authority: authority, gc: gc, waiter: waiter, now: now}
}
