package metadatascrape

import (
	"context"
	"errors"

	metadatamodel "retrom/internal/model/metadata"
)

var (
	ErrMediaInput  = errors.New("MEDIA_INPUT_INVALID")
	ErrMediaBudget = errors.New("MEDIA_BUDGET_INVALID")
)

const MediaRunBudget int64 = 100 << 20

type MediaJob struct {
	ID, State, WorkerID, Input, InputDigest  string
	Scope                                    Subject
	Execution, Attempt, MaxAttempts, Version int64
	Deadline, LeaseUntil, AvailableAt        int64
}
type MediaAsset struct {
	CandidateAsset
	RunID, Status, OwnerKind, OwnerID, OwnerState, PayloadState string
	ParentCancelled                                             bool
	Version, Order, Charged, Reserved                           int64
}
type MediaSnapshot struct {
	Job                 MediaJob
	Asset               MediaAsset
	RunState            string
	Frozen              bool
	Charged, RunVersion int64
	First               bool
}
type MediaOrder struct {
	ID, GameID, Kind          string
	Hits, QueryOrder, Ordinal int
}
type MediaClaim struct {
	JobID, WorkerID                            string
	Execution, Attempt, Version, Now, Deadline int64
	Terminal                                   bool
}
type MediaOutcome struct {
	Claim       MediaClaim
	State, Code string
	Retryable   bool
	Now         int64
}
type MediaReader interface {
	Snapshot(context.Context, string) (MediaSnapshot, error)
	Ordering(context.Context, string) ([]MediaOrder, error)
	Running(context.Context, int64) (int, error)
	RunExecuting(context.Context, string, int64) (bool, error)
}
type MediaLeases interface {
	Claim(context.Context, MediaClaim) error
	Refresh(context.Context, MediaClaim, int64) error
	Requeue(context.Context, MediaClaim, int64) error
	Finish(context.Context, MediaOutcome) error
}
type MediaAssets interface {
	Freeze(context.Context, string, []MediaOrder, int64) error
	Reserve(context.Context, MediaAsset, int64, int64) error
	Account(context.Context, MediaAsset, int64, int64) error
	Publish(context.Context, AssetPublication, int64) error
	Fail(context.Context, MediaAsset, string, string, int64) error
}
type MediaScope struct {
	Read   MediaReader
	Leases MediaLeases
	Assets MediaAssets
}
type MediaRepository interface {
	Recoverable(context.Context, int64) ([]string, error)
	WithWrite(context.Context, func(MediaScope) error) error
}
type MediaProvider interface {
	FetchAssetBounded(context.Context, metadatamodel.AssetReference, int64) (metadatamodel.AssetData, error)
}
