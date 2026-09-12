package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	MaxStartGamelists            = 1000
	MaxStartGamelistBytes  int64 = 8 << 20
	MaxStartGamelistsBytes int64 = 64 << 20
)

type (
	GamelistEvidence struct {
		RelativePath, FactsDigest, ParseState string
		ContentDigest                         *string
		SizeBytes                             int64
	}
	StartSnapshot struct {
		Summary                                Summary
		RootConfigDigest, SourceSnapshotDigest string
		ReleaseYearMax                         int
		Gamelists                              []GamelistEvidence
		TagsValid, TargetsValid, OtherActive   bool
	}
	StartScope struct {
		Read  StartReader
		Write StartWriter
	}
	StartReader interface {
		Current(context.Context, string) (StartSnapshot, error)
	}
	StartWriter interface {
		Queue(context.Context, StartPlan) error
	}
	StartRepository interface {
		Inspect(context.Context, string) (StartSnapshot, error)
		WithStart(context.Context, func(StartScope) error) error
	}
	StartSources interface {
		Select(context.Context, string, string) (SelectedRoot, error)
		VerifyGamelists(context.Context, string, string, []GamelistEvidence) error
	}
	StartPlan struct {
		Before                                          StartSnapshot
		JobID, ExecutionID, AuditID, ActorID, DedupeKey string
		NowMS                                           int64
	}
	Starter struct {
		repository StartRepository
		sources    StartSources
		now        func() time.Time
	}
)

func NewStarter(repository StartRepository, sources StartSources, now func() time.Time) *Starter {
	return &Starter{repository: repository, sources: sources, now: now}
}

func (service *Starter) Start(ctx context.Context, id string, version int64, actorID string) (Summary, bool, error) {
	before, err := service.repository.Inspect(ctx, id)
	if err != nil {
		return Summary{}, false, fmt.Errorf("inspect EmulationStation start: %w", err)
	}
	started, err := startState(before.Summary, version)
	if err != nil {
		return Summary{}, false, err
	}
	if started {
		return before.Summary, false, nil
	}
	if err := readyToStart(before, service.now().UnixMilli()); err != nil {
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

func (service *Starter) verifySource(ctx context.Context, before StartSnapshot) error {
	if before.SourceSnapshotDigest == "" || !validStartEvidence(before.Gamelists) {
		return ErrSourceChanged
	}
	root, err := service.sources.Select(ctx, before.Summary.Root.ID, before.Summary.SourceRelativePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	if root.ID != before.Summary.Root.ID || root.Digest != before.RootConfigDigest {
		return ErrSourceChanged
	}
	if err := service.sources.VerifyGamelists(
		ctx, root.ID, before.Summary.SourceRelativePath, before.Gamelists,
	); err != nil {
		return fmt.Errorf("verify EmulationStation start evidence: %w", err)
	}
	return nil
}

func (service *Starter) queue(ctx context.Context, plan StartPlan, version int64) (Summary, bool, error) {
	var result Summary
	var queued bool
	err := service.repository.WithStart(ctx, func(scope StartScope) error {
		current, err := scope.Read.Current(ctx, plan.Before.Summary.ID)
		if err != nil {
			return fmt.Errorf("reread EmulationStation start: %w", err)
		}
		started, err := startState(current.Summary, version)
		if err != nil {
			return err
		}
		if started {
			result = current.Summary
			return nil
		}
		plan.NowMS = service.now().UnixMilli()
		if err := readyToStart(current, plan.NowMS); err != nil {
			return err
		}
		if !sameStartSnapshot(plan.Before, current) {
			return ErrSourceChanged
		}
		plan.Before = current
		if err := scope.Write.Queue(ctx, plan); err != nil {
			return fmt.Errorf("queue EmulationStation start: %w", err)
		}
		after, err := scope.Read.Current(ctx, current.Summary.ID)
		if err != nil {
			return fmt.Errorf("read queued EmulationStation plan: %w", err)
		}
		result, queued = after.Summary, true
		return nil
	})
	if err != nil {
		return Summary{}, false, fmt.Errorf("finish EmulationStation start: %w", err)
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
			return StartPlan{}, fmt.Errorf("generate EmulationStation start identity: %w", err)
		}
		*target = id.String()
	}
	digest := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00SERVER_EMULATIONSTATION_IMPORT\x00" + before.Summary.ID))
	plan.DedupeKey = hex.EncodeToString(digest[:])
	return plan, nil
}
