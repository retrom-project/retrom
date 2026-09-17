package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/format/importing"
)

type arcadePreparedArchive struct {
	file           model.ImportFile
	machine        string
	classification string
	entries        []importing.ArchiveEntry
	entryByName    map[string]importing.ArchiveEntry
	reason         string
}

func (service *ImportPreparation) ArcadeRequirements(
	ctx context.Context, datID, machine string,
) ([]model.ArcadeROMRequirement, bool, error) {
	facts, err := service.catalog.ArcadeRequirements(ctx, datID, machine)
	if err != nil {
		return nil, false, fmt.Errorf("read arcade preparation requirements: %w", err)
	}
	return SelectedArcadeRequirements(facts), facts.HasDisk, nil
}

func SelectedArcadeRequirements(facts model.ArcadeCatalogRequirements) []model.ArcadeROMRequirement {
	selected := make([]model.ArcadeROMRequirement, 0, len(facts.ROMs))
	for _, rom := range facts.ROMs {
		if rom.Status == "NODUMP" ||
			rom.BIOSName != nil && (facts.DefaultBIOS == nil || *rom.BIOSName != *facts.DefaultBIOS) {
			continue
		}
		selected = append(selected, rom)
	}
	return selected
}

func MatchArcadeRequirements(
	entries map[string]importing.ArchiveEntry,
	requirements []model.ArcadeROMRequirement,
) ([]string, []string, []string) {
	foldedEntries := make(map[string]importing.ArchiveEntry, len(entries))
	for name, entry := range entries {
		foldedEntries[importing.ASCIICaseFold(name)] = entry
	}
	var missing, mismatched, warnings []string
	for _, requirement := range requirements {
		entry, exists := foldedEntries[importing.ASCIICaseFold(requirement.Name)]
		if !exists {
			missing = append(missing, requirement.Name)
			continue
		}
		if !arcadeEntryMatchesRequirement(entry, requirement) {
			mismatched = append(mismatched, requirement.Name)
		}
		if requirement.Status == "BADDUMP" {
			warnings = append(warnings, requirement.Name)
		}
	}
	return missing, mismatched, warnings
}

func arcadeEntryMatchesRequirement(entry importing.ArchiveEntry, requirement model.ArcadeROMRequirement) bool {
	return entry.Size == requirement.Size &&
		(requirement.CRC32 == nil || strings.EqualFold(entry.CRC32, *requirement.CRC32)) &&
		(requirement.SHA1 == nil || strings.EqualFold(entry.SHA1, *requirement.SHA1))
}

func containsMergedArcadeEntries(
	entries map[string]importing.ArchiveEntry,
	requirements []model.ArcadeROMRequirement,
) bool {
	rootEntries := make(map[string]importing.ArchiveEntry, len(entries))
	nestedEntries := make(map[string][]importing.ArchiveEntry)
	for entryPath, entry := range entries {
		if !strings.Contains(entryPath, "/") {
			rootEntries[importing.ASCIICaseFold(entryPath)] = entry
			continue
		}
		base := importing.ASCIICaseFold(filepath.Base(entryPath))
		nestedEntries[base] = append(nestedEntries[base], entry)
	}
	for _, requirement := range requirements {
		name := importing.ASCIICaseFold(requirement.Name)
		if root, exists := rootEntries[name]; exists && arcadeEntryMatchesRequirement(root, requirement) {
			continue
		}
		for _, nested := range nestedEntries[name] {
			if arcadeEntryMatchesRequirement(nested, requirement) {
				return true
			}
		}
	}
	return false
}

func (service *ImportPreparation) arcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]string, []string, []model.ArcadeClosureNode, bool, error) {
	nodes, cyclic, err := LoadArcadeClosure(ctx, service.catalog, datID, machine)
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
	files []model.ImportFile,
	datID string,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
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
	groups := make([]model.PreparedGroup, 0)
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
	files []model.ImportFile,
	datID string,
) ([]arcadePreparedArchive, []model.PreparedArchive, error) {
	prepared := make([]arcadePreparedArchive, 0, len(files))
	archives := make([]model.PreparedArchive, 0, len(files))
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
	file model.ImportFile,
	datID string,
) (arcadePreparedArchive, *model.PreparedArchive, error) {
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
	entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.SHA256), importing.DefaultArchiveLimits())
	if err != nil {
		candidate.reason = ArchiveReason(err)
		return candidate, nil, nil
	}
	candidate.entries = entries
	candidate.entryByName = make(map[string]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		candidate.entryByName[entry.NormalizedPath] = entry
	}
	archive := &model.PreparedArchive{BlobID: file.BlobID, Entries: entries}
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
		if err != nil && !errors.Is(err, model.ErrInvalid) {
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

type arcadeGroupBuilder struct {
	failure          error
	service          *ImportPreparation
	ctx              context.Context
	datID            string
	primary          *arcadePreparedArchive
	byMachine        map[string]*arcadePreparedArchive
	referenced       map[string]struct{}
	parents          []string
	bases            []string
	closure          []model.ArcadeClosureNode
	closureByMachine map[string]model.ArcadeClosureNode
	status           string
	code             string
	missing          []string
	mismatched       []string
	warnings         []string
	dependencies     []map[string]any
	mergedROMSet     bool
	sources          []model.PreparedSource
	validationFiles  []model.PreparedValidationFile
}

func newArcadeGroupBuilder(
	ctx context.Context,
	service *ImportPreparation,
	datID string,
	primary *arcadePreparedArchive,
	byMachine map[string]*arcadePreparedArchive,
	referenced map[string]struct{},
) *arcadeGroupBuilder {
	return &arcadeGroupBuilder{
		service: service, ctx: ctx, datID: datID, primary: primary,
		byMachine: byMachine, referenced: referenced, status: "READY", code: "READY",
		missing: make([]string, 0), mismatched: make([]string, 0), warnings: make([]string, 0),
		dependencies: make([]map[string]any, 0), validationFiles: make([]model.PreparedValidationFile, 0),
		sources: []model.PreparedSource{{
			File: primary.file, Role: "CONTENT", LogicalName: primary.machine + ".zip",
		}},
	}
}

func (builder *arcadeGroupBuilder) build() (model.PreparedGroup, error) {
	builder.loadClosure()
	builder.validatePrimary()
	builder.addDependencies(builder.parents, "PARENT", "PARENT")
	builder.addDependencies(builder.bases, "BIOS_OR_BASE", "BIOS_BUNDLE")
	if builder.mergedROMSet {
		builder.status, builder.code = "BLOCKED", "UNSUPPORTED_MERGED_ROMSET"
	}
	if builder.failure != nil {
		return model.PreparedGroup{}, builder.failure
	}
	return builder.result(), nil
}

func (builder *arcadeGroupBuilder) loadClosure() {
	parents, bases, closure, cyclic, err := builder.service.arcadeDependencyClosure(
		builder.ctx, builder.datID, builder.primary.machine,
	)
	builder.parents, builder.bases, builder.closure = parents, bases, closure
	if err != nil {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		if !errors.Is(err, model.ErrInvalid) {
			builder.failure = errors.Join(builder.failure, err)
		}
	}
	if cyclic {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DEPENDENCY_CYCLE"
	}
	builder.closureByMachine = make(map[string]model.ArcadeClosureNode, len(closure))
	for _, node := range closure {
		builder.closureByMachine[node.Machine] = node
	}
	if err != nil || cyclic {
		builder.parents, builder.bases = nil, nil
	}
}

func (builder *arcadeGroupBuilder) validatePrimary() {
	requirements, hasDisk, err := builder.service.ArcadeRequirements(
		builder.ctx, builder.datID, builder.primary.machine,
	)
	if err != nil {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		if !errors.Is(err, model.ErrInvalid) {
			builder.failure = errors.Join(builder.failure, err)
		}
	} else if hasDisk {
		builder.status, builder.code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
	}
	direct := directArcadeRequirements(builder.primary.entryByName, requirements)
	missing, mismatched, warnings := MatchArcadeRequirements(builder.primary.entryByName, direct)
	builder.mergedROMSet = containsMergedArcadeEntries(builder.primary.entryByName, requirements)
	builder.missing = append(builder.missing, missing...)
	builder.mismatched = append(builder.mismatched, mismatched...)
	builder.warnings = append(builder.warnings, warnings...)
	if len(missing) > 0 || len(mismatched) > 0 {
		builder.status, builder.code = "BLOCKED", "ARCADE_CONTENT_MISSING_ENTRY"
	}
}

func directArcadeRequirements(
	entries map[string]importing.ArchiveEntry,
	requirements []model.ArcadeROMRequirement,
) []model.ArcadeROMRequirement {
	result := make([]model.ArcadeROMRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement.MergeName == nil {
			result = append(result, requirement)
		} else if _, included := entries[requirement.Name]; included {
			result = append(result, requirement)
		}
	}
	return result
}

func (builder *arcadeGroupBuilder) addDependencies(names []string, kind, role string) {
	for _, name := range names {
		builder.addDependency(name, kind, role)
	}
}

func (builder *arcadeGroupBuilder) addDependency(name, kind, role string) {
	node := builder.closureByMachine[name]
	requirements, hasDisk, err := builder.service.ArcadeRequirements(builder.ctx, builder.datID, name)
	requiredEntries := arcadeRequirementNames(requirements)
	if err != nil {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		if !errors.Is(err, model.ErrInvalid) {
			builder.failure = errors.Join(builder.failure, err)
		}
		return
	}
	if hasDisk {
		builder.status, builder.code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
		return
	}
	if containsMergedArcadeEntries(builder.primary.entryByName, requirements) {
		builder.mergedROMSet = true
	}
	contentMissing, contentMismatch, _ := MatchArcadeRequirements(builder.primary.entryByName, requirements)
	if len(contentMissing) == 0 && len(contentMismatch) == 0 {
		builder.appendDependency(node, name, kind, "SATISFIED_BY_CONTENT", requiredEntries)
		return
	}
	companion := builder.byMachine[name]
	if companion == nil || companion.reason != "" {
		builder.recordMissingDependency(node, name, kind, requiredEntries)
		return
	}
	builder.recordExternalDependency(node, name, kind, role, requirements, requiredEntries, companion)
}

func arcadeRequirementNames(requirements []model.ArcadeROMRequirement) []string {
	result := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		result = append(result, requirement.Name)
	}
	return result
}

func (builder *arcadeGroupBuilder) recordMissingDependency(
	node model.ArcadeClosureNode,
	name, kind string,
	requiredEntries []string,
) {
	builder.missing = append(builder.missing, name+".zip")
	if builder.status == "READY" {
		builder.status = "BLOCKED"
		if kind == "PARENT" {
			builder.code = "LAUNCH_PARENT_MISSING"
		} else {
			builder.code = "LAUNCH_BIOS_MISSING"
		}
	}
	builder.appendDependency(node, name, kind, "MISSING", requiredEntries)
}

func (builder *arcadeGroupBuilder) recordExternalDependency(
	node model.ArcadeClosureNode,
	name, kind, role string,
	requirements []model.ArcadeROMRequirement,
	requiredEntries []string,
	companion *arcadePreparedArchive,
) {
	missing, mismatched, warnings := MatchArcadeRequirements(companion.entryByName, requirements)
	if kind == "PARENT" && (len(missing) > 0 || len(mismatched) > 0) {
		builder.missing = append(builder.missing, missing...)
		builder.mismatched = append(builder.mismatched, mismatched...)
		builder.status, builder.code = "BLOCKED", "ARCADE_DEPENDENCY_MISMATCH"
		builder.appendDependency(node, name, kind, "MISMATCH", requiredEntries)
		return
	}
	state := "SATISFIED_EXTERNAL"
	if len(missing) > 0 || len(mismatched) > 0 {
		state = "HASH_WARNING"
		builder.warnings = append(builder.warnings, missing...)
		builder.warnings = append(builder.warnings, mismatched...)
	}
	builder.warnings = append(builder.warnings, warnings...)
	builder.referenced[companion.file.ID] = struct{}{}
	builder.sources = append(builder.sources, model.PreparedSource{
		File: companion.file, Role: "COMPANION", LogicalName: name + ".zip",
	})
	builder.validationFiles = append(builder.validationFiles, model.PreparedValidationFile{
		Role: role, LogicalName: name + ".zip", BlobID: companion.file.BlobID,
		SortOrder: len(builder.validationFiles),
	})
	builder.appendDependency(node, name, kind, state, requiredEntries)
}

func (builder *arcadeGroupBuilder) appendDependency(
	node model.ArcadeClosureNode,
	name, kind, state string,
	requiredEntries []string,
) {
	builder.dependencies = append(builder.dependencies, map[string]any{
		"kind": kind, "machine": name, "requiredBy": node.RequiredBy, "depth": node.Depth,
		"expectedLogicalName": name + ".zip", "state": state,
		"requiredEntryCount": len(requiredEntries), "requiredEntries": requiredEntries,
	})
}

func (builder *arcadeGroupBuilder) result() model.PreparedGroup {
	sort.Strings(builder.missing)
	sort.Strings(builder.mismatched)
	sort.Strings(builder.warnings)
	sortArcadeDependencies(builder.dependencies)
	snapshot, _ := json.Marshal(map[string]any{
		"schemaVersion": corevalidation.SnapshotSchemaVersion, "kind": corevalidation.SnapshotKindArcade,
		"machine": builder.primary.machine, "datVersionId": builder.datID,
		"closure": builder.closure, "dependencies": builder.dependencies,
		"missingEntries": builder.missing, "mismatchedEntries": builder.mismatched,
		"warnings": builder.warnings,
	})
	return model.PreparedGroup{
		Sources: builder.sources, ValidationStatus: builder.status, CompatibilityCode: builder.code,
		DependencySnapshot: string(snapshot), ValidationFiles: builder.validationFiles,
	}
}

func sortArcadeDependencies(dependencies []map[string]any) {
	sort.Slice(dependencies, func(left, right int) bool {
		leftKind, _ := dependencies[left]["kind"].(string)
		rightKind, _ := dependencies[right]["kind"].(string)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		leftDepth, _ := dependencies[left]["depth"].(int)
		rightDepth, _ := dependencies[right]["depth"].(int)
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		leftMachine, _ := dependencies[left]["machine"].(string)
		rightMachine, _ := dependencies[right]["machine"].(string)
		return leftMachine < rightMachine
	})
}

func arcadeDispositions(
	prepared []arcadePreparedArchive,
	referenced map[string]struct{},
) []model.PreparedDisposition {
	result := make([]model.PreparedDisposition, 0, len(prepared))
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
