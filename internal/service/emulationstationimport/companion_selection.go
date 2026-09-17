package emulationstationimport

import (
	"context"
	"fmt"
	"path"
	model "retrom/internal/model/emulationstationimport"
	"slices"
	"strings"
)

func (service *Companions) load(
	ctx context.Context,
	reader model.CompanionReader,
	unit model.Execution,
	id string,
	now int64,
) (model.CompanionOwner, []model.CompanionFile, error) {
	owner, err := reader.Owner(ctx, id)
	if err != nil {
		return model.CompanionOwner{}, nil, fmt.Errorf("read EmulationStation companion owner: %w", err)
	}
	if err := validateCompanionExecution(owner.Before.Execution, unit, now); err != nil {
		return model.CompanionOwner{}, nil, err
	}
	item := owner.Before.Item
	if item.ID != id || item.ImportID != unit.ImportID || item.State != "COPYING" || !validItemVersion(item.Version) {
		return model.CompanionOwner{}, nil, model.ErrVersionConflict
	}
	if !arcadeCompanionItem(item) {
		return owner, []model.CompanionFile{}, nil
	}
	if err := validateCompanionMapping(ctx, reader, owner); err != nil {
		return model.CompanionOwner{}, nil, err
	}
	machine := strings.TrimSuffix(path.Base(item.Files[0].Path), path.Ext(item.Files[0].Path))
	dependencies, err := reader.Dependencies(ctx, item.TargetDATVersionID, machine)
	if err != nil {
		return model.CompanionOwner{}, nil, fmt.Errorf("read EmulationStation companion closure: %w", err)
	}
	if len(dependencies) == 0 {
		return owner, []model.CompanionFile{}, nil
	}
	candidates, err := reader.Candidates(ctx, owner)
	if err != nil {
		return model.CompanionOwner{}, nil, fmt.Errorf("read EmulationStation companion candidates: %w", err)
	}
	return owner, selectCompanionFiles(candidates, dependencies), nil
}

func validateCompanionMapping(ctx context.Context, reader model.CompanionReader, owner model.CompanionOwner) error {
	target, found, err := reader.Target(ctx, owner.Mapping.InstanceID)
	if err != nil {
		return fmt.Errorf("read current EmulationStation companion target: %w", err)
	}
	if !found || owner.CollectionID == "" || owner.MappingVersion < 1 ||
		owner.Mapping.InstanceID != owner.Before.Item.TargetPlatformID ||
		owner.Mapping.PlatformID != owner.Before.Item.TargetPlatformKind ||
		owner.Mapping.DATVersionID == nil || *owner.Mapping.DATVersionID != owner.Before.Item.TargetDATVersionID ||
		!sameCompanionTarget(owner.Mapping, target) {
		return model.ErrVersionConflict
	}
	return nil
}

func sameCompanionTarget(left, right model.MappingTarget) bool {
	leftDAT, rightDAT := left.DATVersionID, right.DATVersionID
	left.DATVersionID, right.DATVersionID = nil, nil
	return left == right && leftDAT != nil && rightDAT != nil && *leftDAT == *rightDAT
}

func selectCompanionFiles(candidates []model.CompanionFile, dependencies []string) []model.CompanionFile {
	required := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		required[dependency] = struct{}{}
	}
	result := make([]model.CompanionFile, 0, len(candidates))
	for _, file := range candidates {
		extension := path.Ext(file.Path)
		if !strings.EqualFold(extension, ".zip") {
			continue
		}
		machine := strings.TrimSuffix(path.Base(file.Path), extension)
		if _, found := required[machine]; found {
			result = append(result, file)
		}
	}
	return result
}

func validateCompanionExecution(before model.LeaseSnapshot, unit model.Execution, now int64) error {
	if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" {
		return model.ErrVersionConflict
	}
	switch ExecutionState(before, unit, now) {
	case model.LeaseActive:
		return nil
	case model.LeaseCancelled:
		return ErrExecutionCancelled
	case model.LeaseDeadline:
		return model.ErrExpired
	case model.LeaseLost:
		return model.ErrVersionConflict
	default:
		return model.ErrVersionConflict
	}
}

func sameCompanionOwner(current, before model.CompanionOwner) bool {
	return current.CollectionID == before.CollectionID && current.MappingVersion == before.MappingVersion &&
		sameCompanionTarget(current.Mapping, before.Mapping) && sameCompanionItem(current.Before.Item, before.Before.Item)
}

func sameCompanionItem(current, before model.ExecutionItem) bool {
	if !slices.Equal(current.Files, before.Files) || !slices.Equal(current.TagIDs, before.TagIDs) {
		return false
	}
	return current.ID == before.ID && current.ImportID == before.ImportID && current.Version == before.Version &&
		current.State == before.State && current.MetadataJSON == before.MetadataJSON &&
		current.ContentKind == before.ContentKind &&
		current.LibraryImportJobID == before.LibraryImportJobID && current.LibraryImportItemID == before.LibraryImportItemID
}
