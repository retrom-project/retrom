package metadatascrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Scheduled struct {
	RunID, JobID string
	Noop         bool
}

func (scheduled Scheduled) ScrapeRunID() string { return scheduled.RunID }
func (scheduled Scheduled) IsNoop() bool        { return scheduled.Noop }

type Scheduler struct {
	repository ScheduleRepository
	runner     ScrapeDispatcher
	now        func() time.Time
}

func NewScheduler(repository ScheduleRepository, runner ScrapeDispatcher, now func() time.Time) *Scheduler {
	return &Scheduler{repository: repository, runner: runner, now: now}
}

func (scheduler *Scheduler) ScheduleImport(
	ctx context.Context,
	scope ScheduleScope,
	itemID, provider string,
) (Scheduled, error) {
	return scheduler.scheduleImport(
		ctx,
		scope,
		itemID,
		provider,
		"metadata-import-v1:"+itemID+":"+provider,
		false,
		scheduler.now().UnixMilli(),
	)
}

func (scheduler *Scheduler) scheduleImport(
	ctx context.Context,
	scope ScheduleScope,
	itemID, provider, dedupe string,
	bypass bool,
	now int64,
) (Scheduled, error) {
	if provider != "HASHEOUS" && provider != "NONE" {
		return Scheduled{}, ErrProviderInvalid
	}
	plan, err := newSchedulePlan(
		Subject{
			Kind: "IMPORT_ITEM",
			ID:   itemID,
		},
		provider,
		dedupe,
		map[string]any{
			"provider":    provider,
			"bypassCache": bypass,
		},
		now,
	)
	if err != nil {
		return Scheduled{}, err
	}
	if err := scope.Writes.Create(ctx, plan); err != nil {
		return Scheduled{}, fmt.Errorf("create import scrape: %w", err)
	}
	if provider == "NONE" {
		return Scheduled{RunID: plan.RunID, JobID: plan.JobID, Noop: true}, nil
	}
	item, err := scope.Subjects.Import(ctx, itemID)
	if err != nil {
		return Scheduled{}, fmt.Errorf("read scrape import subject: %w", err)
	}
	if err := scheduleEvidence(ctx, scope, plan, item.PlatformID); err != nil {
		return Scheduled{}, err
	}
	return Scheduled{RunID: plan.RunID, JobID: plan.JobID}, nil
}

func newSchedulePlan(
	subject Subject,
	provider, dedupe string,
	payload map[string]any,
	now int64,
) (SchedulePlan, error) {
	runID, err := scheduleID()
	if err != nil {
		return SchedulePlan{}, err
	}
	jobID, err := scheduleID()
	if err != nil {
		return SchedulePlan{}, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return SchedulePlan{}, fmt.Errorf("encode scrape payload: %w", err)
	}
	if subject.Kind == "GAME" {
		dedupe += ":" + runID
	}
	digest := sha256.Sum256([]byte(dedupe))
	plan := SchedulePlan{
		Subject: subject, RunID: runID, JobID: jobID, Provider: provider, Dedupe: hex.EncodeToString(digest[:]),
		PayloadJSON: string(
			payloadJSON,
		), JobState: "QUEUED", RunState: "RUNNING", EventJSON: fmt.Sprintf(
			`{"provider":%q}`,
			provider,
		), Now: now,
	}
	if subject.Kind == "GAME" {
		plan.EventJSON = "{}"
	}
	if provider == "NONE" {
		plan.JobState = "SUCCEEDED"
		plan.RunState = "COMPLETED"
		plan.FinishedAt = &now
	}
	return plan, nil
}

func scheduleID() (string, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create scrape identity: %w", err)
	}
	return value.String(), nil
}

func (scheduler *Scheduler) start(ctx context.Context, scheduled Scheduled) {
	if scheduled.Noop || scheduler.runner == nil {
		return
	}
	scheduler.runner.Dispatch(ctx, scheduled.RunID)
}
