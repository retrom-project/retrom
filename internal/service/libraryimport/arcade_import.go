package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/content/arcade"

	"retrom/internal/importing"
)

type arcadePreparedArchive struct {
	file           ImportFile
	machine        string
	classification string
	entries        []importing.ArchiveEntry
	entryByName    map[string]importing.ArchiveEntry
	reason         string
}

func (service *ImportPreparation) ParentRequirements(
	ctx context.Context, datID, machine string,
) (arcade.Requirements, error) {
	requirements, err := arcade.LoadRequirements(ctx, service.catalog, datID, machine)
	if err != nil {
		return arcade.Requirements{}, fmt.Errorf("read Parent entry sources: %w", err)
	}
	return requirements, nil
}

func (service *ImportPreparation) arcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]string, []string, []arcade.ClosureNode, bool, error) {
	nodes, cyclic, err := arcade.LoadClosure(ctx, service.catalog, datID, machine)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("libraryimport/service: %w", err)
	}
	parents := make([]string, 0)
	bases := make([]string, 0)
	for _, node := range nodes {
		switch node.Kind {
		case "PARENT":
			parents = append(parents, node.Machine)
		case "BIOS_OR_BASE":
			bases = append(bases, node.Machine)
		}
	}
	return parents, bases, nodes, cyclic, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (service *ImportPreparation) PrepareArcadeFiles(
	ctx context.Context,
	files []ImportFile,
	datID string,
) ([]PreparedDisposition, []PreparedGroup, []PreparedArchive, error) {
	prepared, archives, err := service.prepareArcadeArchives(ctx, files, datID)
	if err != nil {
		return nil, nil, nil, err
	}
	byMachine := indexedArcadeArchives(prepared)
	dependencyCandidates, err := service.findUploadedArcadeDependencies(ctx, prepared, byMachine, datID)
	if err != nil {
		return nil, nil, nil, err
	}
	referenced := make(map[string]struct{})
	groups := make([]PreparedGroup, 0)
	for index := range prepared {
		primary := &prepared[index]
		if !isPrimaryArcadeArchive(primary, dependencyCandidates) {
			continue
		}
		builder := newArcadeGroupBuilder(ctx, service, datID, primary, byMachine, referenced)
		group, err := builder.build()
		if err != nil {
			return nil, nil, nil, err
		}
		groups = append(groups, group)
	}
	return arcadeDispositions(prepared, referenced), groups, archives, nil
}

func (service *ImportPreparation) prepareArcadeArchives(
	ctx context.Context,
	files []ImportFile,
	datID string,
) ([]arcadePreparedArchive, []PreparedArchive, error) {
	prepared := make([]arcadePreparedArchive, 0, len(files))
	archives := make([]PreparedArchive, 0, len(files))
	for _, file := range files {
		candidate, archive, err := service.prepareArcadeArchive(ctx, file, datID)
		if err != nil {
			return nil, nil, err
		}
		prepared = append(prepared, candidate)
		if archive != nil {
			archives = append(archives, *archive)
		}
	}
	return prepared, archives, nil
}

func (service *ImportPreparation) prepareArcadeArchive(
	ctx context.Context,
	file ImportFile,
	datID string,
) (arcadePreparedArchive, *PreparedArchive, error) {
	candidate := arcadePreparedArchive{
		file: file, machine: strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path)),
	}
	if knownSidecar(file.Path) {
		candidate.reason = "IGNORED_SYSTEM_SIDECAR"
		return candidate, nil, nil
	}
	if !strings.EqualFold(filepath.Ext(file.Path), ".zip") || service.blobs == nil {
		candidate.reason = "UNSUPPORTED_CONTENT_FORMAT"
		return candidate, nil, nil
	}
	entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.FileRecord), importing.DefaultArchiveLimits())
	if err != nil {
		candidate.reason = ArchiveReason(err)
		return candidate, nil, nil
	}
	candidate.entries = entries
	candidate.entryByName = make(map[string]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		candidate.entryByName[entry.NormalizedPath] = entry
	}
	archive := &PreparedArchive{FileRecord: file.FileRecord, Entries: entries}
	if datID == "" {
		candidate.reason = "ARCADE_DAT_UNAVAILABLE"
		return candidate, archive, nil
	}
	classification, found, err := service.catalog.MachineClassification(ctx, datID, candidate.machine)
	if err != nil {
		return arcadePreparedArchive{}, nil, fmt.Errorf("read arcade machine classification: %w", err)
	}
	candidate.classification = classification
	if !found {
		candidate.reason = "ARCADE_MACHINE_NOT_FOUND"
	}
	return candidate, archive, nil
}

func indexedArcadeArchives(prepared []arcadePreparedArchive) map[string]*arcadePreparedArchive {
	result := make(map[string]*arcadePreparedArchive, len(prepared))
	for index := range prepared {
		if prepared[index].classification != "" {
			result[prepared[index].machine] = &prepared[index]
		}
	}
	return result
}

func (service *ImportPreparation) findUploadedArcadeDependencies(
	ctx context.Context,
	prepared []arcadePreparedArchive,
	byMachine map[string]*arcadePreparedArchive,
	datID string,
) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if datID == "" {
		return result, nil
	}
	for index := range prepared {
		candidate := &prepared[index]
		if candidate.reason != "" || candidate.classification != "NORMAL" {
			continue
		}
		parents, bases, _, cyclic, err := service.arcadeDependencyClosure(ctx, datID, candidate.machine)
		if err != nil && !errors.Is(err, arcade.ErrInvalid) {
			return nil, err
		}
		if err != nil || cyclic {
			continue
		}
		for _, machine := range append(parents, bases...) {
			if _, uploaded := byMachine[machine]; uploaded {
				result[machine] = struct{}{}
			}
		}
	}
	return result, nil
}

func isPrimaryArcadeArchive(
	candidate *arcadePreparedArchive,
	dependencyCandidates map[string]struct{},
) bool {
	if candidate.reason != "" || candidate.classification != "NORMAL" {
		return false
	}
	_, dependencyOnly := dependencyCandidates[candidate.machine]
	return !dependencyOnly
}

func newArcadeGroupBuilder(
	ctx context.Context, service *ImportPreparation, datID string, primary *arcadePreparedArchive,
	byMachine map[string]*arcadePreparedArchive, referenced map[string]struct{},
) *arcadeGroupBuilder {
	return &arcadeGroupBuilder{
		ctx: ctx, service: service, datID: datID, primary: primary, byMachine: byMachine, referenced: referenced,
	}
}

type arcadeGroupBuilder struct {
	ctx        context.Context
	service    *ImportPreparation
	datID      string
	primary    *arcadePreparedArchive
	byMachine  map[string]*arcadePreparedArchive
	referenced map[string]struct{}
}

func (builder *arcadeGroupBuilder) build() (PreparedGroup, error) {
	archives := []arcade.Archive{{
		Role: "CONTENT", LogicalName: builder.primary.machine + ".zip",
		FileRecord: builder.primary.file.FileRecord, Entries: builder.primary.entryByName,
	}}
	for _, candidate := range builder.byMachine {
		if candidate == builder.primary || candidate.reason != "" {
			continue
		}
		archives = append(archives, arcade.Archive{
			Role: "COMPANION", LogicalName: candidate.machine + ".zip",
			FileRecord: candidate.file.FileRecord, Entries: candidate.entryByName,
		})
	}
	evaluated, err := arcade.Resolve(
		builder.ctx, builder.service.catalog, arcade.NoInstalledBIOS{}, "", "",
		builder.datID, builder.primary.machine, archives,
	)
	if err != nil {
		return PreparedGroup{}, fmt.Errorf("evaluate import arcade content: %w", err)
	}
	encoded, err := json.Marshal(evaluated.Snapshot)
	if err != nil {
		return PreparedGroup{}, fmt.Errorf("encode prepared arcade dependencies: %w", err)
	}
	group := PreparedGroup{
		Sources: []PreparedSource{{
			File: builder.primary.file, Role: "CONTENT", LogicalName: builder.primary.machine + ".zip",
		}},
		ValidationStatus: evaluated.Status, CompatibilityCode: evaluated.Code,
		DependencySnapshot: string(encoded), ValidationFiles: []PreparedValidationFile{},
	}
	for index, resource := range evaluated.Companions {
		name := strings.TrimSuffix(resource.LogicalName, ".zip")
		candidate := builder.byMachine[name]
		builder.referenced[candidate.file.ID] = struct{}{}
		group.Sources = append(group.Sources, PreparedSource{
			File: candidate.file, Role: "COMPANION", LogicalName: resource.LogicalName,
		})
		group.ValidationFiles = append(group.ValidationFiles, PreparedValidationFile{
			Role: resource.Role, LogicalName: resource.LogicalName, FileRecord: resource.FileRecord, SortOrder: index,
		})
	}
	return group, nil
}

func arcadeDispositions(
	prepared []arcadePreparedArchive,
	referenced map[string]struct{},
) []PreparedDisposition {
	result := make([]PreparedDisposition, 0, len(prepared))
	for index := range prepared {
		candidate := &prepared[index]
		switch {
		case candidate.reason == "IGNORED_SYSTEM_SIDECAR":
			result = append(result, ignoredDisposition(candidate.file))
		case candidate.reason != "":
			result = append(result, rejectedDisposition(candidate.file, candidate.reason))
		case candidate.classification == "NORMAL":
			result = append(result, sourceDisposition(candidate.file))
		default:
			_, used := referenced[candidate.file.ID]
			if used {
				result = append(result, sourceDisposition(candidate.file))
			} else {
				result = append(result, rejectedDisposition(
					candidate.file, "ARCADE_UNUSED_DEPENDENCY_ARCHIVE",
				))
			}
		}
	}
	return result
}
