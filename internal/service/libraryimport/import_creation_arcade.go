package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"sort"

	"retrom/internal/capability/content/corevalidation"
)

type CreationArcadeState struct {
	Tracked                    bool
	Status, Code, SnapshotJSON string
	Dependencies               []corevalidation.BIOSDependency
}

func ResolveCreationArcade(
	ctx context.Context,
	reader model.CreationArcadeReader,
	providerID, targetID, previousSnapshot, previousStatus, previousCode string,
) (CreationArcadeState, error) {
	snapshot, valid := ParseArcadeDraftSnapshot(previousSnapshot)
	if !valid {
		return CreationArcadeState{}, nil
	}
	state := CreationArcadeState{
		Tracked: true, SnapshotJSON: previousSnapshot, Status: previousStatus, Code: previousCode,
		Dependencies: make([]corevalidation.BIOSDependency, 0),
	}
	resolvedNames := make(map[string]struct{})
	for index := range snapshot.Dependencies {
		dependency := &snapshot.Dependencies[index]
		if dependency.Kind != "BIOS_OR_BASE" || dependency.State != "MISSING" {
			continue
		}
		resolved, dependencyState, err := resolveCreationArcadeDependency(
			ctx, reader, providerID, targetID, dependency.Machine+".zip",
		)
		if err != nil {
			return CreationArcadeState{}, err
		}
		if resolved == nil {
			continue
		}
		dependency.State = dependencyState
		if dependencyState == "HASH_WARNING" {
			snapshot.Warnings = append(snapshot.Warnings, dependency.Machine+".zip:"+*resolved.InstallationStatus)
		}
		resolvedNames[dependency.Machine+".zip"] = struct{}{}
		state.Dependencies = append(state.Dependencies, *resolved)
	}
	if len(resolvedNames) == 0 {
		return state, nil
	}
	missing := snapshot.MissingEntries[:0]
	for _, entry := range snapshot.MissingEntries {
		if _, resolved := resolvedNames[entry]; !resolved {
			missing = append(missing, entry)
		}
	}
	snapshot.MissingEntries = missing
	sort.Strings(snapshot.Warnings)
	if previousCode == "LAUNCH_BIOS_MISSING" && len(snapshot.MissingEntries) == 0 {
		state.Status, state.Code = "READY", "READY"
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return CreationArcadeState{}, fmt.Errorf("libraryimport/review: encode arcade BIOS snapshot: %w", err)
	}
	state.SnapshotJSON = string(encoded)
	return state, nil
}

func resolveCreationArcadeDependency(
	ctx context.Context,
	reader model.CreationArcadeReader,
	providerID, targetID, name string,
) (*corevalidation.BIOSDependency, string, error) {
	dependency, found, err := reader.BIOS(ctx, providerID, targetID, name)
	if err != nil {
		return nil, "", fmt.Errorf("read creation arcade BIOS: %w", err)
	}
	if !found {
		return nil, "", nil
	}
	state := "SATISFIED_EXTERNAL"
	if dependency.InstallationStatus == nil || *dependency.InstallationStatus != "MATCHED" {
		state = "HASH_WARNING"
	}
	return &dependency, state, nil
}
