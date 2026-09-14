package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Creation struct {
	repository CreationRepository
	sources    SourceSelector
	now        func() time.Time
}

func NewCreation(
	repository CreationRepository,
	sources SourceSelector,
	now func() time.Time,
) *Creation {
	return &Creation{repository: repository, sources: sources, now: now}
}

func (service *Creation) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	root, err := service.sources.Select(ctx, request.RootID, request.SourceRelativePath)
	if err != nil {
		return Summary{}, fmt.Errorf("select Pegasus source: %w", err)
	}
	plan, err := newCreationPlan(request, root, actorID, service.now().UnixMilli())
	if err != nil {
		return Summary{}, err
	}
	var result Summary
	err = service.repository.WithCreate(ctx, func(writer CreationWriter) error {
		count, err := writer.PendingPlans(ctx)
		if err != nil {
			return fmt.Errorf("count pending Pegasus plans: %w", err)
		}
		if count >= 20 {
			return ErrActive
		}
		result, err = writer.Insert(ctx, plan)
		if err != nil {
			return fmt.Errorf("persist Pegasus plan: %w", err)
		}
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("create Pegasus import: %w", err)
	}
	return result, nil
}

func newCreationPlan(request CreateRequest, root SelectedRoot, actorID string, now int64) (CreationPlan, error) {
	plan := CreationPlan{
		Request:     request,
		Root:        root,
		ActorID:     actorID,
		NowMS:       now,
		ExpiresAtMS: now + (7 * 24 * time.Hour).Milliseconds(),
	}
	for _, target := range []*string{&plan.ImportID, &plan.JobID, &plan.ExecutionID, &plan.AuditID} {
		id, err := uuid.NewV7()
		if err != nil {
			return CreationPlan{}, fmt.Errorf("generate Pegasus creation identity: %w", err)
		}
		*target = id.String()
	}
	digest := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00SERVER_PEGASUS_SCAN\x00" + plan.ImportID))
	plan.DedupeKey = hex.EncodeToString(digest[:])
	return plan, nil
}
