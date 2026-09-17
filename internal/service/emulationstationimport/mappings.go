package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	model "retrom/internal/model/emulationstationimport"
	"time"

	"retrom/internal/model/tagging"
)

type Mappings struct {
	repository model.MappingRepository
	tags       model.MappingTagWriter
	now        func() time.Time
}

func NewMappings(repository model.MappingRepository, tags model.MappingTagWriter, now func() time.Time) *Mappings {
	return &Mappings{repository: repository, tags: tags, now: now}
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
			return fmt.Errorf("read EmulationStation mapping plan: %w", err)
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
			return fmt.Errorf("advance EmulationStation mappings: %w", err)
		}
		result, err = scope.Read.Import(ctx, id)
		if err != nil {
			return fmt.Errorf("read updated EmulationStation mappings: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Summary{}, fmt.Errorf("finish EmulationStation mappings: %w", err)
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
		collection, err := reader.Collection(ctx, mapping.CollectionID)
		if err != nil {
			return nil, fmt.Errorf("read EmulationStation mapping owner: %w", err)
		}
		if collection.ImportID != id || mapping.Action == "IMPORT" && collection.GameCount < 1 {
			return nil, model.ErrInvalid
		}
		change := model.CollectionMapping{ImportID: id, Mapping: mapping, NowMS: now}
		if mapping.Action == "IMPORT" {
			target, found, err := reader.EligibleTarget(ctx, mapping.PlatformInstanceID)
			if err != nil {
				return nil, fmt.Errorf("read EmulationStation mapping target: %w", err)
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
		references, err := service.tags.ReplaceEmulationStationCollectionTags(
			ctx,
			scope.Tags,
			change.Mapping.CollectionID,
			change.Mapping.TagIDs,
			actorID,
			change.NowMS,
		)
		if errors.Is(err, tagging.ErrInvalid) {
			return fmt.Errorf("%w: %w", model.ErrInvalid, err)
		}
		if err != nil {
			return fmt.Errorf("replace EmulationStation mapping tags: %w", err)
		}
		change.Tags = references
		if err := scope.Write.Put(ctx, change); err != nil {
			return fmt.Errorf("save EmulationStation collection mapping: %w", err)
		}
	}
	return nil
}
