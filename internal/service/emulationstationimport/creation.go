package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	model "retrom/internal/model/emulationstationimport"
	"time"

	"github.com/google/uuid"
)

type Creation struct {
	repository model.CreationRepository
	sources    model.SourceSelector
	now        func() time.Time
}

func NewCreation(
	repository model.CreationRepository,
	sources model.SourceSelector,
	now func() time.Time,
) *Creation {
	return &Creation{repository: repository, sources: sources, now: now}
}

func (service *Creation) Create(ctx context.Context, request model.CreateRequest, actorID string) (model.Summary, error) {
	root, err := service.sources.Select(ctx, request.RootID, request.SourceRelativePath)
	if err != nil {
		return model.Summary{}, fmt.Errorf("select EmulationStation source: %w", err)
	}
	plan, err := newCreationPlan(request, root, actorID, service.now())
	if err != nil {
		return model.Summary{}, err
	}
	var result model.Summary
	err = service.repository.WithCreate(ctx, func(writer model.CreationWriter) error {
		count, err := writer.PendingPlans(ctx)
		if err != nil {
			return fmt.Errorf("count pending EmulationStation plans: %w", err)
		}
		if count >= 20 {
			return model.ErrActive
		}
		result, err = writer.Insert(ctx, plan)
		if err != nil {
			return fmt.Errorf("persist EmulationStation plan: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Summary{}, fmt.Errorf("create EmulationStation import: %w", err)
	}
	return result, nil
}

func newCreationPlan(request model.CreateRequest, root model.SelectedRoot, actorID string, now time.Time) (model.CreationPlan, error) {
	plan := model.CreationPlan{
		Request:        request,
		Root:           root,
		ActorID:        actorID,
		NowMS:          now.UnixMilli(),
		ReleaseYearMax: now.UTC().Year() + 1,
		ExpiresAtMS:    now.UnixMilli() + (7 * 24 * time.Hour).Milliseconds(),
	}
	for _, target := range []*string{&plan.ImportID, &plan.JobID, &plan.ExecutionID, &plan.AuditID} {
		id, err := uuid.NewV7()
		if err != nil {
			return model.CreationPlan{}, fmt.Errorf("generate EmulationStation creation identity: %w", err)
		}
		*target = id.String()
	}
	digest := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00SERVER_EMULATIONSTATION_SCAN\x00" + plan.ImportID))
	plan.DedupeKey = hex.EncodeToString(digest[:])
	return plan, nil
}
