package pegasusimport

import (
	"context"
	"errors"
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
	var result model.Summary
	err := service.repository.WithMappings(ctx, func(scope model.MappingScope) error {
		before, err := scope.Read.Import(ctx, id)
		if err != nil {
			return fmt.Errorf("read Pegasus mapping plan: %w", err)
		}
		if before.State != "AWAITING_MAPPING" {
			return model.ErrMapping
		}
		if before.Version != version || version < 1 || version == math.MaxInt64 || before.MappingVersion == math.MaxInt64 {
			return model.ErrVersionConflict
		}
		prepared, err := prepareMappings(ctx, scope.Read, id, mappings, service.now().UnixMilli())
		if err != nil {
			return err
		}
		if actorID == "" {
			actorID = before.CreatedBy.ID
		}
		if err := service.saveMappings(ctx, scope, prepared, actorID); err != nil {
			return err
		}
		if err := scope.Write.Advance(ctx, model.MappingAdvance{Before: before, NowMS: prepared[0].NowMS}); err != nil {
			return fmt.Errorf("advance Pegasus mappings: %w", err)
		}
		result, err = scope.Read.Import(ctx, id)
		if err != nil {
			return fmt.Errorf("read updated Pegasus mappings: %w", err)
		}
		return nil
	})
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

func prepareMappings(
	ctx context.Context,
	reader model.MappingReader,
	id string,
	mappings []model.Mapping,
	now int64,
) ([]model.CollectionMapping, error) {
	result := make([]model.CollectionMapping, 0, len(mappings))
	for _, mapping := range mappings {
		owner, err := reader.CollectionOwner(ctx, mapping.CollectionID)
		if err != nil {
			return nil, fmt.Errorf("read Pegasus mapping owner: %w", err)
		}
		if owner != id {
			return nil, model.ErrInvalid
		}
		change := model.CollectionMapping{ImportID: id, Mapping: mapping, NowMS: now}
		if mapping.Action == "IMPORT" {
			target, found, err := reader.EligibleTarget(ctx, mapping.PlatformInstanceID)
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

func (service *Mappings) saveMappings(
	ctx context.Context,
	scope model.MappingScope,
	changes []model.CollectionMapping,
	actorID string,
) error {
	for _, change := range changes {
		owner := tagging.Owner{Kind: tagging.OwnerPegasusCollection, ID: change.Mapping.CollectionID}
		_, references, err := scope.Tags.ReplaceOwnerReferences(
			ctx, owner, change.Mapping.TagIDs, actorID, change.NowMS,
		)
		if errors.Is(err, tagging.ErrInvalid) {
			return fmt.Errorf("%w: %w", model.ErrInvalid, err)
		}
		if err != nil {
			return fmt.Errorf("replace Pegasus mapping tags: %w", err)
		}
		change.Tags = references
		if err := scope.Write.Put(ctx, change); err != nil {
			return fmt.Errorf("save Pegasus collection mapping: %w", err)
		}
	}
	return nil
}
