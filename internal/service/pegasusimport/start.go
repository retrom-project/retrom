package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

type Starter struct {
	repository StartRepository
	sources    StartSources
	now        func() time.Time
}

func NewStarter(repository StartRepository, sources StartSources, now func() time.Time) *Starter {
	return &Starter{repository: repository, sources: sources, now: now}
}

func (service *Starter) Start(ctx context.Context, id string, version int64, actorID string) (Summary, bool, error) {
	before, err := service.repository.Inspect(ctx, id)
	if err != nil {
		return Summary{}, false, fmt.Errorf("inspect Pegasus start: %w", err)
	}
	if alreadyStarted(before.Summary.State) {
		return before.Summary, false, nil
	}
	if err := readyToStart(before, version, service.now().UnixMilli()); err != nil {
		return Summary{}, false, err
	}
	if err := service.verifySource(ctx, before); err != nil {
		return Summary{}, false, err
	}
	plan, err := newStartPlan(before, actorID)
	if err != nil {
		return Summary{}, false, err
	}
	return service.queue(ctx, plan, version)
}

func alreadyStarted(state string) bool {
	switch state {
	case "QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE":
		return true
	}
	return false
}

func readyToStart(before StartSnapshot, version, now int64) error {
	summary := before.Summary
	if summary.State != "AWAITING_MAPPING" || now >= summary.ExpiresAtMS {
		return ErrExpired
	}
	if summary.Version != version || version < 1 || version == math.MaxInt64 {
		return ErrVersionConflict
	}
	if summary.Counts.MappedCollections+summary.Counts.SkippedCollections != summary.Counts.Collections ||
		!before.TagsValid {
		return ErrMapping
	}
	if summary.Counts.MappedCollections == 0 {
		return ErrNoSelection
	}
	if before.OtherActive {
		return ErrActive
	}
	if summary.ImportJobID != nil {
		return ErrMapping
	}
	return nil
}

func (service *Starter) verifySource(ctx context.Context, before StartSnapshot) error {
	if before.SourceSnapshotDigest == "" || len(before.Metadata) == 0 || len(before.Metadata) > MaxMetadataFiles {
		return ErrSourceChanged
	}
	root, err := service.sources.Select(ctx, before.Summary.Root.ID, before.Summary.SourceRelativePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	if root.ID != before.Summary.Root.ID || root.Digest != before.RootConfigDigest {
		return ErrSourceChanged
	}
	if err := service.sources.VerifyMetadata(
		ctx,
		root.ID,
		before.Summary.SourceRelativePath,
		before.Metadata,
	); err != nil {
		return fmt.Errorf("verify Pegasus start metadata: %w", err)
	}
	return nil
}

func (service *Starter) queue(ctx context.Context, plan StartPlan, version int64) (Summary, bool, error) {
	var result Summary
	var queued bool
	err := service.repository.WithStart(ctx, func(scope StartScope) error {
		current, err := scope.Read.Current(ctx, plan.Before.Summary.ID)
		if err != nil {
			return fmt.Errorf("reread Pegasus start: %w", err)
		}
		if alreadyStarted(current.Summary.State) {
			result = current.Summary
			return nil
		}
		plan.NowMS = service.now().UnixMilli()
		if err := readyToStart(current, version, plan.NowMS); err != nil {
			return err
		}
		if current.RootConfigDigest != plan.Before.RootConfigDigest ||
			current.SourceSnapshotDigest != plan.Before.SourceSnapshotDigest {
			return ErrSourceChanged
		}
		plan.Before = current
		if err := scope.Write.Queue(ctx, plan); err != nil {
			return fmt.Errorf("queue Pegasus start: %w", err)
		}
		if err := scheduleTerminalPayloads(ctx, scope.Payload, plan.Before.Summary.ID, plan.NowMS); err != nil {
			return err
		}
		after, err := scope.Read.Current(ctx, current.Summary.ID)
		if err != nil {
			return fmt.Errorf("read queued Pegasus import: %w", err)
		}
		result, queued = after.Summary, true
		return nil
	})
	if err != nil {
		return Summary{}, false, fmt.Errorf("finish Pegasus start: %w", err)
	}
	return result, queued, nil
}

func newStartPlan(before StartSnapshot, actorID string) (StartPlan, error) {
	if actorID == "" {
		actorID = before.Summary.CreatedBy.ID
	}
	plan := StartPlan{Before: before, ActorID: actorID}
	for _, target := range []*string{&plan.JobID, &plan.ExecutionID, &plan.AuditID} {
		id, err := uuid.NewV7()
		if err != nil {
			return StartPlan{}, fmt.Errorf("generate Pegasus start identity: %w", err)
		}
		*target = id.String()
	}
	digest := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00SERVER_PEGASUS_IMPORT\x00" + before.Summary.ID))
	plan.DedupeKey = hex.EncodeToString(digest[:])
	return plan, nil
}
