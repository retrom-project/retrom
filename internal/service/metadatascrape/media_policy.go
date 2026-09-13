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

func mediaDigest(input string) string {
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])
}

func validateMediaInput(snapshot MediaSnapshot) error {
	job, asset := snapshot.Job, snapshot.Asset
	if job.InputDigest != mediaDigest(job.Input) {
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
		input.Inputs.ResponseID != asset.ResponseID || input.Inputs.SourceDigest != mediaSourceDigest(asset.CandidateAsset) {
		return ErrMediaInput
	}
	return nil
}

func sortMedia(assets []MediaOrder) {
	slices.SortFunc(assets, func(a, b MediaOrder) int {
		return cmp.Or(cmp.Compare(b.Hits, a.Hits), cmp.Compare(a.QueryOrder, b.QueryOrder),
			cmp.Compare(a.GameID, b.GameID), cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Ordinal, b.Ordinal), cmp.Compare(a.ID, b.ID))
	})
}

func mediaOwnerAvailable(snapshot MediaSnapshot) bool {
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

func mediaOwned(snapshot MediaSnapshot, claim MediaClaim) bool {
	job := snapshot.Job
	return job.ID == claim.JobID && job.Execution == claim.Execution &&
		job.Attempt == claim.Attempt && job.WorkerID == claim.WorkerID
}

func mediaActive(snapshot MediaSnapshot, claim MediaClaim, now int64) error {
	if !mediaOwned(snapshot, claim) || snapshot.Job.State != "RUNNING" {
		return ErrExecutionLost
	}
	if snapshot.Job.Deadline <= now {
		return context.DeadlineExceeded
	}
	if snapshot.Job.LeaseUntil <= now {
		return ErrExecutionLost
	}
	if !mediaOwnerAvailable(snapshot) {
		return ErrGameDeleted
	}
	return validateMediaInput(snapshot)
}
