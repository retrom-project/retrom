package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
)

const metadataExecutionTimeout = time.Hour

type (
	WorkerRepository struct {
		database      *sql.DB
		preCommitHook func() error
	}
	workerRecords struct{ transaction *sql.Tx }
)

func NewWorker(database *sql.DB) *WorkerRepository { return &WorkerRepository{database: database} }

func WithWorkerPreCommitHook(repo *WorkerRepository, hook func() error) {
	repo.preCommitHook = hook
}

func (repository *WorkerRepository) Run(ctx context.Context, id string) (metadatascrape.WorkerRun, error) {
	var run metadatascrape.WorkerRun
	err := repository.database.QueryRowContext(
		ctx,
		`SELECT r.id,r.job_id,r.provider,r.state,j.state,j.payload_json,j.execution_no,
 j.version,j.attempt_count,j.max_attempts,
 COALESCE(j.execution_deadline_at_ms,0),COALESCE(j.leased_until_ms,0),j.available_at_ms
 FROM metadata_scrape_runs r JOIN jobs j ON j.id=r.job_id WHERE r.id=?`,
		id,
	).
		Scan(&run.RunID, &run.JobID, &run.Provider, &run.State, &run.JobState, &run.Payload, &run.ExecutionNo,
			&run.Version, &run.AttemptCount, &run.MaxAttempts, &run.Deadline, &run.LeaseUntil, &run.AvailableAt)
	if err != nil {
		return run, fmt.Errorf("query metadata execution: %w", err)
	}
	return run, nil
}

func (repository *WorkerRepository) CommitClaim(
	ctx context.Context, cmd metadatascrape.WorkerClaimCommand,
) (metadatascrape.WorkerClaimResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return metadatascrape.WorkerClaimResult{}, fmt.Errorf("begin metadata claim: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workerRecords{tx}
	run := cmd.Run
	claim := metadatascrape.WorkerClaim{
		RunID: run.RunID, JobID: run.JobID, ExecutionNo: run.ExecutionNo,
		WorkerID: cmd.WorkerID, Version: run.Version, AttemptCount: run.AttemptCount,
		Now: cmd.Now,
	}
	claim.Deadline = run.Deadline
	if claim.Deadline == 0 {
		claim.Deadline = cmd.Now + metadataExecutionTimeout.Milliseconds()
	}
	claim.Terminal = claim.Deadline <= cmd.Now ||
		run.MaxAttempts > 0 && run.AttemptCount >= run.MaxAttempts ||
		run.JobState == "CANCEL_REQUESTED"

	if run.JobState == "RUNNING" && !claim.Terminal {
		available := min(cmd.Now+metadatascrape.MetadataRetryDelay(run.AttemptCount), claim.Deadline)
		if _, err := records.Requeue(ctx, claim, available); err != nil {
			return metadatascrape.WorkerClaimResult{}, fmt.Errorf("reschedule expired metadata lease: %w", err)
		}
		if err := commitWithHook(tx, repository.preCommitHook, "metadata claim"); err != nil {
			return metadatascrape.WorkerClaimResult{}, err
		}
		return metadatascrape.WorkerClaimResult{Claim: claim}, nil
	}

	claimed, err := records.Claim(ctx, claim)
	if err != nil {
		return metadatascrape.WorkerClaimResult{}, fmt.Errorf("claim scrape lease: %w", err)
	}
	if err := commitWithHook(tx, repository.preCommitHook, "metadata claim"); err != nil {
		return metadatascrape.WorkerClaimResult{}, err
	}
	return metadatascrape.WorkerClaimResult{Claim: claim, Claimed: claimed}, nil
}

func (repository *WorkerRepository) CommitRefresh(
	ctx context.Context, cmd metadatascrape.WorkerRefreshCommand,
) (bool, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin metadata refresh: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workerRecords{tx}
	current, err := records.Refresh(ctx, cmd.Claim, cmd.Now)
	if err != nil {
		return false, fmt.Errorf("renew scrape lease: %w", err)
	}
	if err := commitWithHook(tx, repository.preCommitHook, "metadata refresh"); err != nil {
		return false, err
	}
	return current, nil
}

func (repository *WorkerRepository) CommitSettle(
	ctx context.Context, cmd metadatascrape.WorkerSettleCommand,
) (metadatascrape.WorkerSettleResult, error) {
	var result metadatascrape.WorkerSettleResult
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin metadata settle: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workerRecords{tx}
	initial := initialRecords{tx}

	status, err := records.Status(ctx, cmd.Claim, cmd.Now)
	if err != nil {
		return result, fmt.Errorf("read metadata completion ownership: %w", err)
	}
	if status.State == "" {
		return result, nil
	}
	outcome := metadatascrape.WorkerOutcome{
		Claim: cmd.Claim, State: "SUCCEEDED", RunState: "COMPLETED",
		Count: cmd.Count, Now: cmd.Now,
	}
	switch {
	case status.State == "CANCEL_REQUESTED" || status.State == "CANCELLED":
		outcome.State = "CANCELLED"
		outcome.RunState = "CANCELLED"
		err = settleInitialCancel(ctx, initial, cmd.Claim.RunID, status.ParentCancelled, cmd.Now)
	case status.State != "RUNNING" && status.State != "QUEUED":
		return result, nil
	case cmd.Failed || status.Expired:
		code := cmd.Code
		if !cmd.Failed && status.Expired {
			code = "METADATA_EXECUTION_EXPIRED"
			result.Expired = true
		}
		outcome.State = "FAILED"
		outcome.RunState = "FAILED"
		outcome.Code = code
		err = settleInitialFail(ctx, initial, cmd.Claim.RunID, code, cmd.Now)
	default:
		err = settleInitialComplete(ctx, initial, cmd.Claim.RunID, cmd.Now)
	}
	if err != nil {
		return result, err
	}
	if err := records.Finish(ctx, outcome); err != nil {
		return result, err
	}
	return result, commitWithHook(tx, repository.preCommitHook, "metadata settle")
}

func commitWithHook(tx *sql.Tx, hook func() error, label string) error {
	if hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}

func settleInitialComplete(ctx context.Context, initial initialRecords, runID string, now int64) error {
	item, active, err := initialActive(ctx, initial, runID)
	if err != nil || !active {
		return err
	}
	if err := applyInitialCandidate(ctx, initial, runID, item.ItemID, now); err != nil {
		return err
	}
	return advanceInitialReview(ctx, initial, item, now)
}

func settleInitialFail(ctx context.Context, initial initialRecords, runID, code string, now int64) error {
	item, active, err := initialActive(ctx, initial, runID)
	if err != nil || !active {
		return err
	}
	change := metadatascrape.InitialProgress(item, now)
	stage := "SCRAPING"
	change.ItemState = "FAILED_RETRYABLE"
	change.FailedDelta = 1
	change.FailedStage = &stage
	change.ErrorCode = &code
	change.JobState = "RUNNING"
	if item.Running == 1 {
		change.JobState = "PARTIAL_FAILURE"
	}
	if err := initial.Advance(ctx, change); err != nil {
		return fmt.Errorf("fail initial review progress: %w", err)
	}
	return nil
}

func settleInitialCancel(
	ctx context.Context, initial initialRecords, runID string, parentCancelled bool, now int64,
) error {
	item, active, err := initialActive(ctx, initial, runID)
	if err != nil || !active {
		return err
	}
	if !parentCancelled {
		return advanceInitialReview(ctx, initial, item, now)
	}
	change := metadatascrape.InitialProgress(item, now)
	change.ItemState = "CANCELLED"
	change.CancelledDelta = 1
	change.JobState = "CANCEL_REQUESTED"
	if item.Running == 1 {
		change.JobState = "CANCELLED"
	}
	if err := initial.Advance(ctx, change); err != nil {
		return fmt.Errorf("cancel initial scrape progress: %w", err)
	}
	return nil
}

func initialActive(
	ctx context.Context, initial initialRecords, runID string,
) (metadatascrape.InitialImport, bool, error) {
	item, found, err := initial.Import(ctx, runID)
	if err != nil {
		return metadatascrape.InitialImport{}, false, fmt.Errorf("read initial scrape owner: %w", err)
	}
	if !found || item.ItemState != "SCRAPING" {
		return item, false, nil
	}
	if item.Running < 1 {
		return metadatascrape.InitialImport{}, false, metadatascrape.ErrInitialProgressState
	}
	return item, true, nil
}

func advanceInitialReview(
	ctx context.Context, initial initialRecords, item metadatascrape.InitialImport, now int64,
) error {
	change := metadatascrape.InitialProgress(item, now)
	change.ItemState = "REVIEW_PENDING"
	change.ReviewDelta = 1
	change.JobState = "RUNNING"
	if item.Running == 1 {
		change.JobState = "REVIEW_PENDING"
		if item.Failed > 0 || item.Rejected > 0 {
			change.JobState = "PARTIAL_FAILURE"
		}
	}
	if err := initial.Advance(ctx, change); err != nil {
		return fmt.Errorf("complete initial review progress: %w", err)
	}
	return nil
}

func applyInitialCandidate(ctx context.Context, initial initialRecords, runID, itemID string, now int64) error {
	candidates, err := initial.Candidates(ctx, runID)
	if err != nil {
		return fmt.Errorf("read initial scrape candidates: %w", err)
	}
	candidate, found := metadatascrape.SelectInitialCandidate(candidates)
	if !found {
		return nil
	}
	draft, err := initial.Draft(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read initial review draft: %w", err)
	}
	metadata, title, err := metadatascrape.MergeInitialReviewMetadata(draft.MetadataJSON, candidate.MetadataJSON)
	if err != nil {
		return fmt.Errorf("merge initial review metadata: %w", err)
	}
	assets, err := initial.ReadyAssets(ctx, candidate.ID)
	if err != nil {
		return fmt.Errorf("read initial candidate assets: %w", err)
	}
	change := metadatascrape.InitialDraftChange{
		ItemID: itemID, DraftID: draft.ID, CandidateID: candidate.ID,
		MetadataJSON: metadata, Title: title, Now: now,
	}
	metadatascrape.SelectInitialAssets(&change, assets)
	if err := initial.Apply(ctx, change); err != nil {
		return fmt.Errorf("apply initial scrape candidate: %w", err)
	}
	return nil
}

func workerChanged(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("update metadata execution: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count metadata executions: %w", err)
	}
	return n == 1, nil
}

func (records workerRecords) event(ctx context.Context, id, kind, data string, now int64) error {
	_, err := records.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 SELECT id,scope_type,scope_id,?,?,? FROM jobs WHERE id=?`,
		kind,
		data,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("insert metadata execution event: %w", err)
	}
	return nil
}
