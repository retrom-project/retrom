package libraryimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/capability/security/authn"
)

type MetadataSeeder struct {
	repository model.MetadataRepository
	now        func() time.Time
}

func NewMetadataSeeder(repository model.MetadataRepository, now func() time.Time) *MetadataSeeder {
	return &MetadataSeeder{repository: repository, now: now}
}

func (service *MetadataSeeder) Seed(
	ctx context.Context, itemID string, metadata model.ServerMetadata, maximumYear int,
) (int64, []model.ServerMetadataWarning, error) {
	before, err := service.repository.LoadCurrentMetadata(ctx, itemID)
	if err != nil {
		return 0, nil, fmt.Errorf("seed server review metadata: %w", fmt.Errorf("read server review metadata: %w", err))
	}
	input := service.buildSeedInput(ctx, itemID, metadata, maximumYear)
	plan, warnings, err := model.BuildMetadataSeed(before, input)
	if err != nil {
		return 0, nil, err
	}
	if !plan.Changed {
		return plan.ResultVersion, warnings, nil
	}
	if err := service.repository.CommitMetadataChange(ctx, plan.Change); err != nil {
		return 0, nil, fmt.Errorf("seed server review metadata: %w", fmt.Errorf("save server review metadata: %w", err))
	}
	return plan.ResultVersion, warnings, nil
}

// SeedInScope participates in the caller's transaction. Its result becomes
// durable only when the owner commits the entire handoff.
func (service *MetadataSeeder) SeedInScope(
	ctx context.Context, scope model.MetadataScope, itemID string, metadata model.ServerMetadata, maximumYear int,
) (int64, []model.ServerMetadataWarning, error) {
	before, err := scope.CurrentMetadata(ctx, itemID)
	if err != nil {
		return 0, nil, fmt.Errorf("read server review metadata: %w", err)
	}
	input := service.buildSeedInput(ctx, itemID, metadata, maximumYear)
	plan, warnings, err := model.BuildMetadataSeed(before, input)
	if err != nil {
		return 0, nil, err
	}
	if !plan.Changed {
		return plan.ResultVersion, warnings, nil
	}
	if err := scope.SaveMetadata(ctx, plan.Change); err != nil {
		return 0, nil, fmt.Errorf("save server review metadata: %w", err)
	}
	return plan.ResultVersion, warnings, nil
}

func (service *MetadataSeeder) buildSeedInput(ctx context.Context, itemID string, metadata model.ServerMetadata, maximumYear int) model.MetadataSeedInput {
	id, _ := uuid.NewV7()
	input := model.MetadataSeedInput{
		ItemID: itemID, Metadata: metadata, MaximumYear: maximumYear,
		NowMS: service.now().UnixMilli(), AuditID: id.String(),
		ActorKind: "SYSTEM",
	}
	label := "release-setup"
	input.ActorLabel = &label
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		input.ActorKind = "USER"
		input.ActorUserID = &principal.UserID
		input.ActorLabel = nil
	}
	return input
}
