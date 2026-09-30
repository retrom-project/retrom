package gamevariant

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/content/arcade"
)

// An alternate core evaluates the published source against its own active DAT.
func (s *Service) PrepareArcade(ctx context.Context, snapshot Snapshot) (*ArcadePreparation, error) {
	if s.arcade == nil || snapshot.Source.ActiveDATVersionID == nil {
		return nil, ErrBlocked
	}
	source := snapshot.Source
	machine := strings.TrimSuffix(filepath.Base(source.ValidationLogicalName), filepath.Ext(source.ValidationLogicalName))
	files := make([]arcade.SourceFile, 0, len(snapshot.GameFiles))
	for _, file := range snapshot.GameFiles {
		if (file.Role == "CONTENT" || file.Role == "COMPANION") && strings.EqualFold(filepath.Ext(file.LogicalName), ".zip") {
			files = append(files, arcade.SourceFile{Role: file.Role, LogicalName: file.LogicalName, FileRecord: file.FileRecord})
		}
	}
	evaluated, err := s.arcade.Prepare(ctx, files, *source.ActiveDATVersionID, machine, source.ValidationLogicalName)
	if err != nil {
		return nil, fmt.Errorf("prepare alternate arcade core: %w", err)
	}
	return preparedArcadeResult(evaluated, source)
}

func preparedArcadeResult(result arcade.Result, source Source) (*ArcadePreparation, error) {
	if !arcadeResultAcceptable(result, source) {
		return nil, ErrBlocked
	}
	frozen := result.Snapshot
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return nil, fmt.Errorf("encode alternate arcade snapshot: %w", err)
	}
	prepared := &ArcadePreparation{Snapshot: string(encoded)}
	for _, file := range result.Companions {
		prepared.Files = append(prepared.Files, File{
			Role: file.Role, FileRecord: file.FileRecord,
			LogicalName: file.LogicalName, SortOrder: file.SortOrder,
		})
	}
	for _, dependency := range frozen.Dependencies {
		required, err := json.Marshal(dependency.RequiredEntries)
		if err != nil {
			return nil, fmt.Errorf("encode alternate arcade dependency: %w", err)
		}
		prepared.Dependencies = append(prepared.Dependencies, ArcadeDependency{
			Kind: dependency.Kind, Machine: dependency.Machine, State: dependency.State,
			RequiredEntriesJSON: string(required),
		})
	}
	return prepared, nil
}

func arcadeResultAcceptable(result arcade.Result, source Source) bool {
	if result.Status != "READY" && result.Status != "HASH_WARNING" &&
		(result.Status != "BLOCKED" || result.Code != "LAUNCH_BIOS_MISSING") {
		return false
	}
	frozen := result.Snapshot
	valid := frozen.SchemaVersion == 1 && frozen.Kind == "ARCADE" &&
		frozen.Dependencies != nil && frozen.Closure != nil
	machine := strings.TrimSuffix(filepath.Base(source.ValidationLogicalName), filepath.Ext(source.ValidationLogicalName))
	return valid && frozen.DatVersionID == *source.ActiveDATVersionID && frozen.Machine == machine &&
		len(frozen.MismatchedEntries) == 0 && arcadeMissingOnlyBIOS(frozen)
}

func arcadeMissingOnlyBIOS(snapshot arcade.Snapshot) bool {
	allowed := make(map[string]bool)
	for _, dependency := range snapshot.Dependencies {
		if dependency.Kind == "BIOS_OR_BASE" && dependency.State == "MISSING" {
			allowed[dependency.Machine+".zip"] = true
		}
	}
	for _, name := range snapshot.MissingEntries {
		if !allowed[name] {
			return false
		}
	}
	return true
}
