package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

func (run *creationCommit) checkInputs(ctx context.Context, scope ImportCreationScope) error {
	if source := run.options.Source; source != nil {
		before, err := NewSourceOwnership(run.service.settings.Now).Revalidate(
			ctx,
			scope.Ownership,
			source.Intent,
			source.Before,
			run.plan.Target.ID,
		)
		if err != nil {
			return creationError("check inputs", err)
		}
		if before.UploadID != run.plan.Upload.ID {
			return ErrVersionConflict
		}
		run.sourceBefore = before
	}
	current, err := readAdmissionFacts(ctx, scope.Facts, run.plan.Request)
	if err != nil {
		return fmt.Errorf("read creation inputs: %w", err)
	}
	if current.Upload != run.plan.Upload || !slices.Equal(current.Files, run.plan.Files) {
		return ErrVersionConflict
	}
	target := current.Target
	if target.ProviderID == "" {
		bindings, err := scope.Facts.Bindings(ctx, ImportBindingQuery{PlatformID: target.PlatformID, CoreID: target.CoreID})
		if err != nil {
			return fmt.Errorf("revalidate prepared binding: %w", err)
		}
		for _, binding := range bindings {
			if binding.BindingID == run.plan.Target.BindingID {
				target = bindImportTarget(target, binding)
				break
			}
		}
	}
	if !sameCreationTarget(target, run.plan.Target) {
		return ErrVersionConflict
	}

	if run.options.Queued != nil {
		return run.checkQueued(ctx, scope)
	}
	return nil
}

func sameCreationTarget(left, right ImportTarget) bool {
	return left.ID == right.ID && left.Version == right.Version && left.PlatformID == right.PlatformID &&
		left.DefaultCoreID == right.DefaultCoreID && left.CoreID == right.CoreID &&
		left.BindingID == right.BindingID &&
		left.ProviderID == right.ProviderID &&
		left.TargetID == right.TargetID &&
		left.DeliveryProfile == right.DeliveryProfile &&
		left.Policy.Digest() == right.Policy.Digest()
}

func (run *creationCommit) checkQueued(ctx context.Context, scope ImportCreationScope) error {
	current, err := scope.Headers.Queued(ctx, run.options.Queued.JobID)
	if err != nil {
		return fmt.Errorf("read queued creation ownership: %w", err)
	}
	expected := run.options.Queued
	now := run.service.settings.Now().UnixMilli()
	if !ImportExecutionCurrent(*expected, current, now) || current.JobState != "RUNNING" ||
		current.ImportState != "RUNNING" ||
		current.ParentVersion < 1 ||
		current.ParentVersion == math.MaxInt64 ||
		current.Execution.DeadlineMS <= now {
		return ErrVersionConflict
	}
	if current.UploadID != run.plan.Upload.ID || current.UploadVersion != run.plan.Upload.Version ||
		current.UploadDigest != run.plan.Upload.ManifestDigest {
		return ErrVersionConflict
	}
	if err := run.checkQueuedDocuments(current); err != nil {
		return creationError("check queued", err)
	}
	run.header.Queued = &current
	return nil
}

func (run *creationCommit) checkQueuedDocuments(current CreationQueuedSnapshot) error {
	if !MatchesImportDocumentDigest(current.RequestJSON, current.RequestDigest) ||
		!MatchesImportDocumentDigest(current.TargetJSON, current.TargetDigest) {
		return ErrInvalid
	}
	var request QueuedImportRequest
	var target ImportTargetSnapshot
	if err := json.Unmarshal([]byte(current.RequestJSON), &request); err != nil {
		return fmt.Errorf("decode queued creation request: %w", err)
	}
	if err := json.Unmarshal([]byte(current.TargetJSON), &target); err != nil {
		return fmt.Errorf("decode queued creation target: %w", err)
	}
	plan := run.plan
	requested := request.Request
	if request.SchemaVersion != 1 || requested.UploadID != plan.Request.UploadID ||
		requested.TargetPlatformInstanceID != plan.Target.ID || requested.MetadataProvider != plan.Request.MetadataProvider ||
		!slices.Equal(requested.TagIDs, plan.Request.TagIDs) ||
		!creationTargetAllowed(target, run.options.Queued.Target, plan.Target) {
		return ErrVersionConflict
	}

	_, mode, err := NormalizeImportRequest(requested)
	if err != nil {
		return fmt.Errorf("normalize queued creation request: %w", err)
	}
	mode = NormalizeTargetImportMode(plan.Target.PlatformID, mode)
	if mode != plan.ContentMode {
		return ErrVersionConflict
	}
	return nil
}

// ImportExecutionCurrent checks the immutable execution identity and live lease.
// Deadline equality retains the original budget; each operation chooses its terminal policy.
func ImportExecutionCurrent(expected QueuedImportExecution, current CreationQueuedSnapshot, now int64) bool {
	return (current.JobState == "RUNNING" || current.JobState == "CANCEL_REQUESTED") &&
		current.JobVersion > 0 && current.JobVersion < math.MaxInt64 && current.LeaseUntilMS > now &&
		expected.ImportID != "" && expected.WorkerID != "" && expected.ExecutionNo > 0 && expected.Attempt > 0 &&
		sameImportExecutionIdentity(expected, current.Execution)
}

func sameImportExecutionIdentity(expected, actual QueuedImportExecution) bool {
	return expected.ImportID == actual.ImportID && expected.JobID == actual.JobID &&
		expected.WorkerID == actual.WorkerID &&
		expected.ExecutionNo == actual.ExecutionNo && expected.Attempt == actual.Attempt &&
		expected.StartedAtMS == actual.StartedAtMS &&
		expected.DeadlineMS == actual.DeadlineMS && expected.ActorUserID == actual.ActorUserID
}

func creationTargetAllowed(snapshot, frozen ImportTargetSnapshot, target ImportTarget) bool {
	if snapshot.SchemaVersion != 1 || frozen.SchemaVersion != 1 ||
		snapshot.PlatformInstanceID != frozen.PlatformInstanceID ||
		snapshot.PlatformInstanceVersion != frozen.PlatformInstanceVersion || snapshot.PlatformID != frozen.PlatformID ||
		snapshot.DefaultCoreID != frozen.DefaultCoreID || !slices.Equal(snapshot.Targets, frozen.Targets) {
		return false
	}
	return snapshot.PlatformInstanceVersion == target.Version && snapshot.PlatformID == target.PlatformID &&
		snapshot.DefaultCoreID == target.DefaultCoreID && snapshot.PlatformInstanceID == target.ID &&
		slices.Contains(snapshot.Targets, TargetImportGuard(target))
}
