package metadatascrape

import (
	"context"
	model "retrom/internal/model/metadatascrape"
	"time"
)

type mediaMemory struct {
	snapshot    model.MediaSnapshot
	publication model.AssetPublication
	outcome     model.MediaOutcome
	failure     error
	running     int
}

func newMediaMemory() (*mediaMemory, error) {
	asset := model.CandidateAsset{ID: "asset", CandidateID: "candidate", ResponseID: "response"}
	plan, err := newMediaJob(model.Subject{Kind: "GAME", ID: "game"}, "run", asset)
	if err != nil {
		return nil, err
	}
	asset.MediaJobID = plan.JobID
	return &mediaMemory{snapshot: model.MediaSnapshot{
		Job: model.MediaJob{
			ID: plan.JobID, State: "QUEUED", Scope: plan.Scope, Input: plan.InputJSON, InputDigest: plan.InputDigest,
			Execution: 1, Version: 1, MaxAttempts: 4,
		}, Asset: model.MediaAsset{
			CandidateAsset: asset, RunID: "run", Status: "PENDING",
			OwnerKind: "GAME", OwnerID: "game", OwnerState: "PUBLISHED", Version: 1,
		}, RunState: "COMPLETED", Frozen: true, First: true,
	}}, nil
}

func (memory *mediaMemory) CommitWrite(ctx context.Context, work func(model.MediaScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	before := *memory
	err := work(model.MediaScope{Read: memory, Leases: memory, Assets: memory})
	if err != nil {
		*memory = before
	}
	return err
}

func (memory *mediaMemory) Recoverable(context.Context, int64) ([]string, error) {
	return []string{memory.snapshot.Job.ID}, nil
}

func (memory *mediaMemory) Snapshot(context.Context, string) (model.MediaSnapshot, error) {
	return memory.snapshot, nil
}
func (*mediaMemory) Ordering(context.Context, string) ([]model.MediaOrder, error) { return nil, nil }
func (memory *mediaMemory) Running(context.Context, int64) (int, error)           { return memory.running, nil }

func (memory *mediaMemory) Claim(_ context.Context, claim model.MediaClaim) error {
	memory.snapshot.Job.WorkerID = claim.WorkerID
	memory.snapshot.Job.Attempt = claim.Attempt
	if !claim.Terminal {
		memory.snapshot.Job.State = "RUNNING"
		memory.snapshot.Job.Deadline = claim.Deadline
		memory.snapshot.Job.LeaseUntil = claim.Now + 60000
	}
	return nil
}
func (*mediaMemory) Refresh(context.Context, model.MediaClaim, int64) error { return nil }
func (*mediaMemory) Requeue(context.Context, model.MediaClaim, int64) error { return nil }
func (memory *mediaMemory) Finish(_ context.Context, outcome model.MediaOutcome) error {
	memory.outcome = outcome
	return nil
}
func (*mediaMemory) Freeze(context.Context, string, []model.MediaOrder, int64) error { return nil }
func (memory *mediaMemory) Reserve(_ context.Context, _ model.MediaAsset, amount, _ int64) error {
	memory.snapshot.Charged += amount
	memory.snapshot.Asset.Reserved = amount
	memory.snapshot.Asset.Charged += amount
	memory.snapshot.Asset.Status = "FETCHING"
	return nil
}

func (memory *mediaMemory) Account(_ context.Context, asset model.MediaAsset, received, _ int64) error {
	memory.snapshot.Charged -= asset.Reserved - received
	memory.snapshot.Asset.Charged -= asset.Reserved - received
	memory.snapshot.Asset.Reserved = 0
	return nil
}

func (memory *mediaMemory) Publish(_ context.Context, value model.AssetPublication, _ int64) error {
	memory.publication = value
	return memory.failure
}

func (memory *mediaMemory) Fail(_ context.Context, _ model.MediaAsset, _, _ string, _ int64) error {
	return memory.failure
}
func mediaUnitNow() time.Time { return time.UnixMilli(100) }

func (*mediaMemory) RunExecuting(context.Context, string, int64) (bool, error) { return false, nil }
