package metadatascrape

import (
	"context"
	"time"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type mediaMemory struct {
	snapshot    metadatascrapemodel.MediaSnapshot
	publication metadatascrapemodel.AssetPublication
	outcome     metadatascrapemodel.MediaOutcome
	failure     error
	running     int
}

func newMediaMemory() (*mediaMemory, error) {
	asset := metadatascrapemodel.CandidateAsset{ID: "asset", CandidateID: "candidate", ResponseID: "response"}
	plan, err := newMediaJob(metadatascrapemodel.Subject{Kind: "GAME", ID: "game"}, "run", asset)
	if err != nil {
		return nil, err
	}
	asset.MediaJobID = plan.JobID
	return &mediaMemory{snapshot: metadatascrapemodel.MediaSnapshot{
		Job: metadatascrapemodel.MediaJob{
			ID: plan.JobID, State: "QUEUED", Scope: plan.Scope, Input: plan.InputJSON, InputDigest: plan.InputDigest,
			Execution: 1, Version: 1, MaxAttempts: 4,
		}, Asset: metadatascrapemodel.MediaAsset{
			CandidateAsset: asset, RunID: "run", Status: "PENDING",
			OwnerKind: "GAME", OwnerID: "game", OwnerState: "PUBLISHED", Version: 1,
		}, RunState: "COMPLETED", Frozen: true, First: true,
	}}, nil
}

func (memory *mediaMemory) WithWrite(ctx context.Context, work func(metadatascrapemodel.MediaScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	before := *memory
	err := work(metadatascrapemodel.MediaScope{Read: memory, Leases: memory, Assets: memory})
	if err != nil {
		*memory = before
	}
	return err
}

func (memory *mediaMemory) Recoverable(context.Context, int64) ([]string, error) {
	return []string{memory.snapshot.Job.ID}, nil
}

func (memory *mediaMemory) Snapshot(context.Context, string) (metadatascrapemodel.MediaSnapshot, error) {
	return memory.snapshot, nil
}

func (*mediaMemory) Ordering(context.Context, string) ([]metadatascrapemodel.MediaOrder, error) {
	return nil, nil
}

func (memory *mediaMemory) Running(context.Context, int64) (int, error) { return memory.running, nil }

func (memory *mediaMemory) Claim(_ context.Context, claim metadatascrapemodel.MediaClaim) error {
	memory.snapshot.Job.WorkerID = claim.WorkerID
	memory.snapshot.Job.Attempt = claim.Attempt
	if !claim.Terminal {
		memory.snapshot.Job.State = "RUNNING"
		memory.snapshot.Job.Deadline = claim.Deadline
		memory.snapshot.Job.LeaseUntil = claim.Now + 60000
	}
	return nil
}

func (*mediaMemory) Refresh(context.Context, metadatascrapemodel.MediaClaim, int64) error { return nil }

func (*mediaMemory) Requeue(context.Context, metadatascrapemodel.MediaClaim, int64) error { return nil }

func (memory *mediaMemory) Finish(_ context.Context, outcome metadatascrapemodel.MediaOutcome) error {
	memory.outcome = outcome
	return nil
}

func (*mediaMemory) Freeze(context.Context, string, []metadatascrapemodel.MediaOrder, int64) error {
	return nil
}

func (memory *mediaMemory) Reserve(_ context.Context, _ metadatascrapemodel.MediaAsset, amount, _ int64) error {
	memory.snapshot.Charged += amount
	memory.snapshot.Asset.Reserved = amount
	memory.snapshot.Asset.Charged += amount
	memory.snapshot.Asset.Status = "FETCHING"
	return nil
}

func (memory *mediaMemory) Account(_ context.Context, asset metadatascrapemodel.MediaAsset, received, _ int64) error {
	memory.snapshot.Charged -= asset.Reserved - received
	memory.snapshot.Asset.Charged -= asset.Reserved - received
	memory.snapshot.Asset.Reserved = 0
	return nil
}

func (memory *mediaMemory) Publish(_ context.Context, value metadatascrapemodel.AssetPublication, _ int64) error {
	memory.publication = value
	return memory.failure
}

func (memory *mediaMemory) Fail(_ context.Context, _ metadatascrapemodel.MediaAsset, _, _ string, _ int64) error {
	return memory.failure
}
func mediaUnitNow() time.Time { return time.UnixMilli(100) }

func (*mediaMemory) RunExecuting(context.Context, string, int64) (bool, error) { return false, nil }
