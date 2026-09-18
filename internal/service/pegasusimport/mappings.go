package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/pegasusimport"

	"retrom/internal/model/tagging"
)

type Mappings struct {
	repository model.MappingRepository
	now        func() time.Time
}

func NewMappings(repository model.MappingRepository, now func() time.Time) *Mappings {
	return &Mappings{repository: repository, now: now}
}

func (service *Mappings) Update(
	ctx context.Context,
	id string,
	version int64,
	mappings []model.Mapping,
	actorID string,
) (model.Summary, error) {
	if !validMappingBatch(mappings) {
		return model.Summary{}, model.ErrInvalid
	}
	before, err := service.repository.LoadImportSummary(ctx, id)
	if err != nil {
		return model.Summary{}, fmt.Errorf("finish Pegasus mappings: %w", fmt.Errorf("read Pegasus mapping plan: %w", err))
	}
	if before.State != "AWAITING_MAPPING" {
		return model.Summary{}, fmt.Errorf("finish Pegasus mappings: %w", model.ErrMapping)
	}
	if before.Version != version || version < 1 || version == math.MaxInt64 || before.MappingVersion == math.MaxInt64 {
		return model.Summary{}, fmt.Errorf("finish Pegasus mappings: %w", model.ErrVersionConflict)
	}
	prepared, err := service.prepareMappings(ctx, id, mappings, service.now().UnixMilli())
	if err != nil {
		return model.Summary{}, fmt.Errorf("finish Pegasus mappings: %w", err)
	}
	if actorID == "" {
		actorID = before.CreatedBy.ID
	}
	batch := model.MappingBatch{
		ImportID: id,
		Entries:  service.buildMappingEntries(prepared, actorID),
		Advance:  model.MappingAdvance{Before: before, NowMS: prepared[0].NowMS},
	}
	result, err := service.repository.CommitMappingBatch(ctx, batch)
	if err != nil {
		return model.Summary{}, fmt.Errorf("finish Pegasus mappings: %w", err)
	}
	return result, nil
}

func validMappingBatch(mappings []model.Mapping) bool {
	if len(mappings) < 1 || len(mappings) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, mapping := range mappings {
		if mapping.CollectionID == "" || seen[mapping.CollectionID] || mapping.TagIDs == nil {
			return false
		}
		seen[mapping.CollectionID] = true
		switch mapping.Action {
		case "SKIP":
			if mapping.PlatformInstanceID != "" || len(mapping.TagIDs) != 0 {
				return false
			}
		case "IMPORT":
			if mapping.PlatformInstanceID == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (service *Mappings) prepareMappings(
	ctx context.Context,
	id string,
	mappings []model.Mapping,
	now int64,
) ([]model.CollectionMapping, error) {
	result := make([]model.CollectionMapping, 0, len(mappings))
	for _, mapping := range mappings {
		owner, err := service.repository.LoadCollectionOwner(ctx, mapping.CollectionID)
		if err != nil {
			return nil, fmt.Errorf("read Pegasus mapping owner: %w", err)
		}
		if owner != id {
			return nil, model.ErrInvalid
		}
		change := model.CollectionMapping{ImportID: id, Mapping: mapping, NowMS: now}
		if mapping.Action == "IMPORT" {
			target, found, err := service.repository.LoadEligibleTarget(ctx, mapping.PlatformInstanceID)
			if err != nil {
				return nil, fmt.Errorf("read Pegasus mapping target: %w", err)
			}
			if !found {
				return nil, model.ErrInvalid
			}
			change.Target = &target
		}
		result = append(result, change)
	}
	return result, nil
}

func (service *Mappings) buildMappingEntries(
	changes []model.CollectionMapping, actorID string,
) []model.MappingBatchEntry {
	entries := make([]model.MappingBatchEntry, len(changes))
	for i, change := range changes {
		entries[i] = model.MappingBatchEntry{
			Change:  change,
			Owner:   tagging.Owner{Kind: tagging.OwnerPegasusCollection, ID: change.Mapping.CollectionID},
			TagIDs:  change.Mapping.TagIDs,
			ActorID: actorID,
		}
	}
	return entries
}
