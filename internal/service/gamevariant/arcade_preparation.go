package gamevariant

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	libraryimport "retrom/internal/service/libraryimport"
)

// An alternate Arcade core must validate the published ZIP against its own
// active DAT. The import preparer already owns ROM hashes and parent closure.
func (s *Service) PrepareArcade(ctx context.Context, snapshot Snapshot) (*ArcadePreparation, error) {
	if s.arcade == nil || snapshot.Source.ActiveDATVersionID == nil {
		return nil, ErrBlocked
	}
	source := snapshot.Source
	_, groups, _, err := s.arcade.PrepareArcadeFiles(
		ctx, arcadeImportFiles(snapshot.GameFiles), *source.ActiveDATVersionID,
	)
	if err != nil {
		return nil, fmt.Errorf("prepare alternate arcade core: %w", err)
	}
	for _, group := range groups {
		if len(group.Sources) != 0 && group.Sources[0].LogicalName == source.ValidationLogicalName {
			return preparedArcadeGroup(group, source)
		}
	}
	return nil, ErrBlocked
}

func arcadeImportFiles(gameFiles []File) []libraryimport.ImportFile {
	files := make([]libraryimport.ImportFile, 0, len(gameFiles))
	for _, file := range gameFiles {
		if file.Role != "CONTENT" && file.Role != "COMPANION" {
			continue
		}
		if !strings.EqualFold(filepath.Ext(file.LogicalName), ".zip") {
			continue
		}
		files = append(files, libraryimport.ImportFile{
			ID: file.LogicalName, Path: file.LogicalName, FileRecord: file.FileRecord,
			SHA256: file.Digest, Size: file.SizeBytes,
		})
	}
	return files
}

func preparedArcadeGroup(group libraryimport.PreparedGroup, source Source) (*ArcadePreparation, error) {
	if group.ValidationStatus != "READY" && group.ValidationStatus != "HASH_WARNING" &&
		(group.ValidationStatus != "BLOCKED" || group.CompatibilityCode != "LAUNCH_BIOS_MISSING") {
		return nil, ErrBlocked
	}
	frozen, valid := libraryimport.ParseArcadeDraftSnapshot(group.DependencySnapshot)
	machine := strings.TrimSuffix(filepath.Base(source.ValidationLogicalName), filepath.Ext(source.ValidationLogicalName))
	if !valid || frozen.DatVersionID != *source.ActiveDATVersionID || frozen.Machine != machine ||
		len(frozen.MismatchedEntries) != 0 || !arcadeMissingOnlyBIOS(frozen) {
		return nil, ErrBlocked
	}
	prepared := &ArcadePreparation{Snapshot: group.DependencySnapshot}
	for _, file := range group.ValidationFiles {
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

func arcadeMissingOnlyBIOS(snapshot libraryimport.ArcadeDraftSnapshot) bool {
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
