package metadatascrape

import (
	"context"
	"time"

	model "retrom/internal/model/metadatascrape"
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

func (memory *mediaMemory) Recoverable(context.Context, int64) ([]string, error) {
	return []string{memory.snapshot.Job.ID}, nil
}

func (memory *mediaMemory) CommitClaim(
	_ context.Context, cmd model.MediaClaimCommand,
) (model.MediaClaimResult, error) {
	snapshot := memory.snapshot
	now := cmd.Now
	result := model.MediaClaimResult{
		Claim: model.MediaClaim{
			JobID: cmd.JobID, WorkerID: cmd.WorkerID, Execution: snapshot.Job.Execution,
			Attempt: snapshot.Job.Attempt, Version: snapshot.Job.Version, Now: now,
			Deadline: snapshot.Job.Deadline,
		},
		Asset: snapshot.Asset,
	}
	if !model.MediaClaimable(snapshot.Job, now) {
		return result, nil
	}
	code, cause := model.MediaTerminalCause(snapshot, now)
	result.Code = code
	result.Failed = cause != nil
	result.Claim.Terminal = cause != nil
	if result.Claim.Terminal {
		result.Acquired = true
		return result, nil
	}
	running := memory.running
	if running >= 2 || snapshot.RunState == "RUNNING" || snapshot.Job.AvailableAt > now {
		return result, nil
	}
	result.Limit = model.MediaRunBudget - snapshot.Charged
	if result.Limit <= 0 {
		result.Code = "ASSET_RUN_BUDGET_EXCEEDED"
		result.Failed = true
		result.Claim.Terminal = true
		result.Acquired = true
		return result, nil
	}
	result.Claim.Attempt++
	if result.Claim.Deadline == 0 {
		result.Claim.Deadline = now + (30 * time.Minute).Milliseconds()
	}
	result.Acquired = true
	memory.snapshot.Job.WorkerID = cmd.WorkerID
	memory.snapshot.Job.Attempt = result.Claim.Attempt
	memory.snapshot.Job.State = "RUNNING"
	memory.snapshot.Job.Deadline = result.Claim.Deadline
	memory.snapshot.Job.LeaseUntil = now + 60000
	memory.snapshot.Charged += result.Limit
	memory.snapshot.Asset.Reserved = result.Limit
	memory.snapshot.Asset.Charged += result.Limit
	memory.snapshot.Asset.Status = "FETCHING"
	return result, nil
}

func (memory *mediaMemory) CommitRefresh(context.Context, model.MediaRefreshCommand) error {
	return nil
}

func (memory *mediaMemory) CommitSettle(
	_ context.Context, cmd model.MediaSettleCommand,
) (model.MediaSettleResult, error) {
	outcome := model.MediaCompletion(memory.snapshot, cmd)
	memory.outcome = outcome
	if outcome.State == "SUCCEEDED" {
		pub := cmd.Publication
		pub.Now = outcome.Now
		memory.publication = pub
		if err := memory.failure; err != nil {
			return model.MediaSettleResult{}, err
		}
	} else if outcome.State == "FAILED" && memory.failure != nil {
		return model.MediaSettleResult{}, memory.failure
	}
	return model.MediaSettleResult{State: outcome.State}, nil
}

func (memory *mediaMemory) CommitAccount(
	_ context.Context, cmd model.MediaAccountCommand,
) error {
	memory.snapshot.Charged -= cmd.Limit - cmd.Received
	memory.snapshot.Asset.Charged -= cmd.Limit - cmd.Received
	memory.snapshot.Asset.Reserved = 0
	return nil
}

func mediaUnitNow() time.Time { return time.UnixMilli(100) }
