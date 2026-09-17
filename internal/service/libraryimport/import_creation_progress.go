package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/model/importprogress"
	validation "retrom/internal/service/corevalidation"
	"retrom/internal/service/payloadrelease"
)

func (run *creationCommit) prepareHeader(ctx context.Context, scope model.ImportCreationScope) error {
	header := &run.header
	header.Plan = run.plan
	for _, disposition := range run.plan.Dispositions {
		switch disposition.Disposition {
		case "IGNORED":
			header.Ignored++
		case "REJECTED":
			header.Rejected++
		}
	}
	projection, running, pending, err := creationProgress(
		run.plan.Request.MetadataProvider, int64(len(run.groups)), header.Rejected, header.NowMS,
	)
	if err != nil {
		return creationError("prepare header", err)
	}
	header.State, header.CompletedAtMS = projection.State, projection.CompletedAtMS
	header.Running, header.Pending = running, pending
	header.ItemState = "REVIEW_PENDING"
	if run.plan.Request.MetadataProvider == "HASHEOUS" {
		header.ItemState = "SCRAPING"
	}
	catalog, err := validation.New(scope.BIOS).Catalog(ctx, run.plan.Target.ProviderID, run.plan.Target.TargetID)
	if err != nil {
		return creationError("prepare header", err)
	}
	target := run.plan.Target
	config := map[string]any{
		"schemaVersion":                 2,
		"contentMode":                   run.plan.ContentMode,
		"platformInstanceId":            target.ID,
		"platformInstanceVersion":       target.Version,
		"platformId":                    target.PlatformID,
		"defaultCoreId":                 target.DefaultCoreID,
		"resolvedCoreId":                target.CoreID,
		"providerId":                    target.ProviderID,
		"targetId":                      target.TargetID,
		"contentPolicyDigest":           target.Policy.Digest(),
		"datVersionId":                  creationOptional(run.plan.DATVersionID),
		"biosRequirements":              catalog,
		"metadataProviderConfigVersion": 1,
		"tags":                          run.tags,
	}
	if run.plan.ContentMode == contentcapability.ModeMultiDisc {
		capability := contentcapability.Resolve(target.PlatformID, true, run.service.settings.MultiDiscEnabled, target.Policy)
		config["multiDisc"] = capability.MultiDisc
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode creation config: %w", err)
	}
	digest := sha256.Sum256(encoded)
	dedupe := sha256.Sum256([]byte("import:" + run.plan.Request.UploadID))
	header.ConfigJSON = string(encoded)
	header.ConfigDigest, header.DedupeKey = hex.EncodeToString(digest[:]), hex.EncodeToString(dedupe[:])
	return nil
}

func creationProgress(
	provider string,
	remaining, rejected, now int64,
) (importprogress.Projection, int64, int64, error) {
	counts := importprogress.Counts{Rejected: rejected}
	if provider == "HASHEOUS" {
		counts.Running = remaining
	} else {
		counts.ReviewPending = remaining
	}
	projection, err := importprogress.Project(importprogress.Snapshot{Counts: counts, Started: true}, now)
	if err != nil {
		return importprogress.Projection{}, 0, 0, fmt.Errorf("project creation progress: %w", err)
	}
	return projection, counts.Running, counts.ReviewPending, nil
}

func (run *creationCommit) complete(ctx context.Context, scope model.ImportCreationScope) error {
	state := run.header.State
	if run.duplicates > 0 {
		projection, running, pending, err := creationProgress(
			run.plan.Request.MetadataProvider,
			int64(len(run.groups))-run.duplicates,
			run.header.Rejected,
			run.header.NowMS,
		)
		if err != nil {
			return creationError("complete", err)
		}
		var files int64
		for id, count := range run.duplicateCounts {
			if count == run.sourceCounts[id] {
				files++
			}
		}
		change := model.CreationAggregate{
			ImportID:        run.header.ImportID,
			ExpectedVersion: run.parentVersion,
			ExpectedPending: run.header.Pending,
			Running:         running,
			Pending:         pending,
			Discarded:       run.duplicates,
			ImportedItems:   run.duplicates,
			ImportedFiles:   files,
			Projection:      projection,
			NowMS:           run.header.NowMS,
		}
		if err := scope.Finish.Aggregate(ctx, change); err != nil {
			return creationError("complete", err)
		}
		run.parentVersion++
		state = projection.State
	}
	if err := run.successEvent(ctx, scope); err != nil {
		return creationError("complete", err)
	}
	if run.header.Queued != nil {
		if err := scope.Finish.FinishJob(
			ctx,
			model.CreationJobFinish{Before: *run.header.Queued, NowMS: run.service.settings.Now().UnixMilli()},
		); err != nil {
			return creationError("complete", err)
		}
	}
	if err := run.schedulePayload(ctx, scope); err != nil {
		return creationError("complete", err)
	}
	if err := run.resolveReconfiguration(ctx, scope); err != nil {
		return creationError("complete", err)
	}
	run.result.Created = model.ServerCreated{
		ImportJobID: run.header.ImportID,
		JobID:       run.header.JobID,
		State:       state,
		ItemCount:   len(run.groups),
	}
	return nil
}

func (run *creationCommit) successEvent(ctx context.Context, scope model.ImportCreationScope) error {
	code := "NOT_APPLICABLE"
	if run.plan.ContentMode == contentcapability.ModeMultiDisc {
		switch {
		case len(run.groups) == 0:
			code = "REJECTED"
		case run.header.Rejected > 0:
			code = "PARTIAL_REJECTED"
		default:
			code = "MATCHED"
		}
	}
	execution, attempt := int64(1), int64(1)
	if run.options.Queued != nil {
		execution, attempt = run.options.Queued.ExecutionNo, run.options.Queued.Attempt
	}
	encoded, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "contentMode": run.plan.ContentMode, "executionNo": execution, "attempt": attempt,
		"parserResultCode": code, "itemCount": len(run.groups), "rejectedFileCount": run.header.Rejected,
	})
	if err != nil {
		return fmt.Errorf("encode creation completion event: %w", err)
	}
	return creationError("success event", scope.Reviews.Events(
		ctx,
		[]model.CreationEvent{
			{
				JobID:     run.header.JobID,
				ScopeType: "IMPORT_GROUP",
				ScopeID:   run.header.ImportID,
				Kind:      "SUCCEEDED",
				DataJSON:  string(encoded),
				NowMS:     run.header.NowMS,
			},
		},
	))
}

func (run *creationCommit) resolveReconfiguration(ctx context.Context, scope model.ImportCreationScope) error {
	request := run.options.Reconfiguration
	if request == nil {
		return nil
	}
	before, err := scope.Finish.Reconfiguration(ctx, request.ImportID)
	if err != nil {
		return fmt.Errorf("read reconfiguration authority: %w", err)
	}
	if before.Version != request.Version || before.Progress.State != "PARTIAL_FAILURE" || len(request.FileIDs) == 0 {
		return model.ErrVersionConflict
	}
	ids := slices.Clone(request.FileIDs)
	slices.Sort(ids)
	if len(slices.Compact(ids)) != len(ids) {
		return model.ErrInvalid
	}
	for _, id := range ids {
		if !slices.Contains(before.Files, id) {
			return model.ErrVersionConflict
		}
	}
	after := before.Progress
	after.Counts.ResolvedRejected += int64(len(ids))
	projection, err := importprogress.Project(after, run.header.NowMS)
	if err != nil {
		return creationError("resolve reconfiguration", err)
	}
	change := model.CreationFileResolution{
		Before:        before,
		ReplacementID: run.header.ImportID,
		FileIDs:       ids,
		Actor:         run.actor,
		Projection:    projection,
		NowMS:         run.header.NowMS,
	}
	if err := scope.Finish.ResolveFiles(ctx, change); err != nil {
		return creationError("resolve reconfiguration", err)
	}
	_, err = payloadrelease.NewScheduler(nil).TerminalImport(ctx, scope.Payload, request.ImportID, run.header.NowMS)
	return creationError("resolve reconfiguration", err)
}
