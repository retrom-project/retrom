package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"

	"github.com/google/uuid"
)

type Starter struct {
	repository model.StartRepository
	sources    model.FrozenSources
	now        func() time.Time
}

func NewStarter(repository model.StartRepository, sources model.FrozenSources, now func() time.Time) *Starter {
	return &Starter{repository: repository, sources: sources, now: now}
}

func (service *Starter) Start(ctx context.Context, id string, version int64, actorID string) (
	model.Summary,
	bool,
	error,
) {
	before, err := service.repository.Inspect(ctx, id)
	if err != nil {
		return model.Summary{}, false, fmt.Errorf("inspect EmulationStation start: %w", err)
	}
	started, err := startState(before.Summary, version)
	if err != nil {
		return model.Summary{}, false, err
	}
	if started {
		return before.Summary, false, nil
	}
	if err := readyToStart(before, service.now().UnixMilli()); err != nil {
		return model.Summary{}, false, err
	}
	if err := verifyFrozenSource(ctx, service.sources, before.Summary, before.FrozenSourceSnapshot); err != nil {
		return model.Summary{}, false, err
	}
	plan, err := newStartPlan(before, actorID)
	if err != nil {
		return model.Summary{}, false, err
	}
	return service.queue(ctx, plan, version)
}

func (service *Starter) queue(ctx context.Context, plan model.StartPlan, version int64) (model.Summary, bool, error) {
	var result model.Summary
	var queued bool
	err := service.repository.WithStart(ctx, func(scope model.StartScope) error {
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
		if !sameFrozenSource(
			plan.Before.Summary,
			current.Summary,
			plan.Before.FrozenSourceSnapshot,
			current.FrozenSourceSnapshot,
		) {
			return model.ErrSourceChanged
		}
		plan.Before = current
		if err := scope.Write.Queue(ctx, plan); err != nil {
			return fmt.Errorf("queue EmulationStation start: %w", err)
		}
		if err := scheduleTerminalPayloads(ctx, scope.Payload, plan.Before.Summary.ID, plan.NowMS); err != nil {
			return err
		}
		after, err := scope.Read.Current(ctx, current.Summary.ID)
		if err != nil {
			return fmt.Errorf("read queued EmulationStation plan: %w", err)
		}
		result, queued = after.Summary, true
		return nil
	})
	if err != nil {
		return model.Summary{}, false, fmt.Errorf("finish EmulationStation start: %w", err)
	}
	return result, queued, nil
}

func newStartPlan(before model.StartSnapshot, actorID string) (model.StartPlan, error) {
	if actorID == "" {
		actorID = before.Summary.CreatedBy.ID
	}
	plan := model.StartPlan{Before: before, ActorID: actorID}
	for _, target := range []*string{&plan.JobID, &plan.ExecutionID, &plan.AuditID} {
		id, err := uuid.NewV7()
		if err != nil {
			return model.StartPlan{}, fmt.Errorf("generate EmulationStation start identity: %w", err)
		}
		*target = id.String()
	}
	digest := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00SERVER_EMULATIONSTATION_IMPORT\x00" + before.Summary.ID))
	plan.DedupeKey = hex.EncodeToString(digest[:])
	return plan, nil
}
