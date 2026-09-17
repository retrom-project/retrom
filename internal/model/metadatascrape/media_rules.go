package metadatascrape

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// MediaClaimable returns whether a media job can be claimed at the given time.
func MediaClaimable(job MediaJob, now int64) bool {
	switch job.State {
	case "QUEUED":
		return true
	case "RUNNING", "CANCEL_REQUESTED":
		return job.LeaseUntil <= now || job.Deadline <= now
	default:
		return false
	}
}

// MediaTerminalCause returns a failure code and error when the snapshot
// indicates the job should not continue.
func MediaTerminalCause(snapshot MediaSnapshot, now int64) (string, error) {
	job := snapshot.Job
	switch {
	case job.State == "CANCEL_REQUESTED":
		return "MEDIA_CANCELLED", context.Canceled
	case !MediaOwnerAvailable(snapshot):
		return "MEDIA_OWNER_UNAVAILABLE", ErrGameDeleted
	case job.Deadline > 0 && job.Deadline <= now:
		return "MEDIA_EXECUTION_EXPIRED", context.DeadlineExceeded
	case job.Attempt >= job.MaxAttempts:
		return "MEDIA_ATTEMPTS_EXHAUSTED", ErrAttemptsExhausted
	}
	if err := ValidateMediaInput(snapshot); err != nil {
		return "MEDIA_INPUT_INVALID", err
	}
	if snapshot.Asset.Status == "READY" || snapshot.Asset.Status == "CANCELLED" {
		return "MEDIA_ASSET_STATE_INVALID", ErrAssetStateConflict
	}
	return "", nil
}

// MediaOwnerAvailable returns true when the asset's owner entity still
// permits media processing.
func MediaOwnerAvailable(snapshot MediaSnapshot) bool {
	asset := snapshot.Asset
	if asset.ID == "" || asset.OwnerKind != snapshot.Job.Scope.Kind || asset.OwnerID != snapshot.Job.Scope.ID {
		return false
	}
	if asset.OwnerKind == "GAME" {
		return asset.OwnerState == "PUBLISHED"
	}
	if asset.OwnerKind != "IMPORT_ITEM" || asset.ParentCancelled || asset.PayloadState != "RETAINED" {
		return false
	}
	return asset.OwnerState == "SCRAPING" || asset.OwnerState == "REVIEW_PENDING" || asset.OwnerState == "FAILED_RETRYABLE"
}

// MediaOwned returns true when the snapshot matches the given claim.
func MediaOwned(snapshot MediaSnapshot, claim MediaClaim) bool {
	job := snapshot.Job
	return job.ID == claim.JobID && job.Execution == claim.Execution &&
		job.Attempt == claim.Attempt && job.WorkerID == claim.WorkerID
}

// MediaActive validates that the claimed execution is still healthy.
func MediaActive(snapshot MediaSnapshot, claim MediaClaim, now int64) error {
	if !MediaOwned(snapshot, claim) || snapshot.Job.State != "RUNNING" {
		return ErrExecutionLost
	}
	if snapshot.Job.Deadline <= now {
		return context.DeadlineExceeded
	}
	if snapshot.Job.LeaseUntil <= now {
		return ErrExecutionLost
	}
	if !MediaOwnerAvailable(snapshot) {
		return ErrGameDeleted
	}
	return ValidateMediaInput(snapshot)
}

// ValidateMediaInput checks the structural integrity of a media job's input.
func ValidateMediaInput(snapshot MediaSnapshot) error {
	job, asset := snapshot.Job, snapshot.Asset
	if job.InputDigest != MediaDigest(job.Input) {
		return ErrMediaInput
	}
	var input MediaInputEnvelope
	if err := json.Unmarshal([]byte(job.Input), &input); err != nil {
		return errors.Join(ErrMediaInput, fmt.Errorf("decode media input: %w", err))
	}
	execution, err := uuid.Parse(input.ExecutionID)
	if err != nil {
		return errors.Join(ErrMediaInput, fmt.Errorf("parse media input execution: %w", err))
	}
	if execution.Version() != 7 || input.SchemaVersion != 1 || input.Kind != "MEDIA_FETCH" ||
		input.Scope.Type != job.Scope.Kind || input.Scope.ID != job.Scope.ID ||
		input.Inputs.AssetID != asset.ID || input.Inputs.RunID != asset.RunID ||
		input.Inputs.ResponseID != asset.ResponseID || input.Inputs.SourceDigest != MediaSourceDigest(asset.CandidateAsset) {
		return ErrMediaInput
	}
	return nil
}

// MediaDigest returns the SHA-256 hex digest of an input string.
func MediaDigest(input string) string {
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])
}

// MediaSourceDigest computes a deterministic digest of a candidate asset's
// source identity fields.
func MediaSourceDigest(asset CandidateAsset) string {
	source := sha256.New()
	for _, field := range []string{
		asset.ID, asset.CandidateID, asset.ResponseID, asset.Reference.ProviderAssetID,
		asset.Reference.Path, asset.Reference.Kind, fmt.Sprint(asset.Reference.Ordinal),
	} {
		_, _ = fmt.Fprintf(source, "%d:%s", len(field), field)
	}
	return hex.EncodeToString(source.Sum(nil))
}

// MediaCompletion determines the outcome of a media execution based on the
// current snapshot and the settle command.
func MediaCompletion(
	snapshot MediaSnapshot, cmd MediaSettleCommand,
) MediaOutcome {
	outcome := MediaOutcome{Claim: cmd.Claim, State: "SUCCEEDED", Now: cmd.Now}
	if snapshot.Job.State == "CANCEL_REQUESTED" || !MediaOwnerAvailable(snapshot) {
		outcome.State = "CANCELLED"
		outcome.Code = "MEDIA_CANCELLED"
		return outcome
	}
	failed := cmd.Failed
	retryable := cmd.Retryable
	if !failed {
		if err := MediaActive(snapshot, cmd.Claim, cmd.Now); err != nil {
			failed = true
			retryable = true
		}
	}
	if failed {
		outcome.State = "FAILED"
		outcome.Code = cmd.Code
		outcome.Retryable = retryable
		if outcome.Code == "" {
			outcome.Code = "MEDIA_EXECUTION_INTERRUPTED"
		}
		if snapshot.Job.Deadline > 0 && snapshot.Job.Deadline <= cmd.Now {
			outcome.Code = "MEDIA_EXECUTION_EXPIRED"
		}
	}
	return outcome
}

// SortMedia sorts asset ordering entries by hit count (desc), then query order,
// game ID, kind, ordinal, and asset ID.
func SortMedia(assets []MediaOrder) {
	slices.SortFunc(assets, func(a, b MediaOrder) int {
		return cmp.Or(cmp.Compare(b.Hits, a.Hits), cmp.Compare(a.QueryOrder, b.QueryOrder),
			cmp.Compare(a.GameID, b.GameID), cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Ordinal, b.Ordinal), cmp.Compare(a.ID, b.ID))
	})
}
