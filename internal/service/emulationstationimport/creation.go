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

func (service *Creation) Create(
	ctx context.Context,
	request model.CreateRequest,
	actorID string,
) (model.Summary, error) {
	root, err := service.sources.Select(ctx, request.RootID, request.SourceRelativePath)
	if err != nil {
		return model.Summary{}, fmt.Errorf("select EmulationStation source: %w", err)
	}
	plan, err := newCreationPlan(request, root, actorID, service.now())
	if err != nil {
		return model.Summary{}, err
	}
	count, err := service.repository.LoadPendingPlanCount(ctx)
	if err != nil {
		return model.Summary{}, fmt.Errorf("create EmulationStation import: %w", err)
	}
	if count >= 20 {
		return model.Summary{}, model.ErrActive
	}
	result, err := service.repository.CommitCreation(ctx, plan)
	if err != nil {
		return model.Summary{}, fmt.Errorf("create EmulationStation import: %w", err)
	}
	return result, nil
}

func newCreationPlan(
	request model.CreateRequest,
	root model.SelectedRoot,
	actorID string,
	now time.Time,
) (model.CreationPlan, error) {
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
