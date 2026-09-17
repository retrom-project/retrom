package metadatascrape

import (
	"context"
	"errors"

	"retrom/internal/adapter/metadata/hasheous"
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

type MediaClaimCommand struct {
	JobID, WorkerID string
	Now             int64
}
type MediaClaimResult struct {
	Claim    MediaClaim
	Asset    MediaAsset
	Limit    int64
	Acquired bool
	Code     string
	Failed   bool
}
type MediaRefreshCommand struct {
	Claim MediaClaim
	Now   int64
}
type MediaSettleCommand struct {
	Claim       MediaClaim
	Publication AssetPublication
	Code        string
	Failed      bool
	Retryable   bool
	Now         int64
}
type MediaSettleResult struct {
	State string
}
type MediaAccountCommand struct {
	Claim    MediaClaim
	Received int64
	Limit    int64
	Now      int64
}

type MediaRepository interface {
	Recoverable(context.Context, int64) ([]string, error)
	CommitClaim(context.Context, MediaClaimCommand) (MediaClaimResult, error)
	CommitRefresh(context.Context, MediaRefreshCommand) error
	CommitSettle(context.Context, MediaSettleCommand) (MediaSettleResult, error)
	CommitAccount(context.Context, MediaAccountCommand) error
}
type MediaProvider interface {
	FetchAssetBounded(context.Context, hasheous.AssetRef, int64) (hasheous.AssetData, error)
}
