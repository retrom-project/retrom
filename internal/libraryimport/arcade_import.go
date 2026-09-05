package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/importing"
)

type arcadePreparedArchive struct {
	file           importSourceFile
	machine        string
	classification string
	entries        []importing.ArchiveEntry
	entryByName    map[string]importing.ArchiveEntry
	reason         string
}

type arcadeROMRequirement struct {
	name, status string
	size         int64
	crc32, sha1  sql.NullString
	mergeName    sql.NullString
}

func (service *Service) arcadeRequirements(
	ctx context.Context,
	datID, machine string,
) ([]arcadeROMRequirement, bool, error) {
	return arcadeRequirementsWithQueryer(ctx, service.database, datID, machine)
}

type arcadeCatalogQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func arcadeRequirementsWithQueryer(
	ctx context.Context,
	queryer arcadeCatalogQueryer,
	datID, machine string,
) ([]arcadeROMRequirement, bool, error) {
	var defaultBIOS sql.NullString
	_ = queryer.QueryRowContext(ctx, `
SELECT bios_name
FROM dat_bios_sets
WHERE dat_version_id=?
AND machine_name=?
AND is_default=1
`, datID, machine).
		Scan(&defaultBIOS)
	rows, err := queryer.QueryContext(
		ctx,
		`
SELECT name,
size_bytes,
crc32,
sha1,
COALESCE(status,
'GOOD'),
bios_name,
merge_name
FROM dat_rom_entries
WHERE dat_version_id=?
AND machine_name=?
ORDER BY ordinal
`,
		datID,
		machine,
	)
	if err != nil {
		return nil, false, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	requirements := make([]arcadeROMRequirement, 0)
	for rows.Next() {
		var requirement arcadeROMRequirement
		var biosName sql.NullString
		if err := rows.Scan(
			&requirement.name,
			&requirement.size,
			&requirement.crc32,
			&requirement.sha1,
			&requirement.status,
			&biosName,
			&requirement.mergeName,
		); err != nil {
			return nil, false, fmt.Errorf("libraryimport/service: %w", err)
		}
		if requirement.status == "NODUMP" ||
			biosName.Valid && (!defaultBIOS.Valid || biosName.String != defaultBIOS.String) {
			continue
		}
		requirements = append(requirements, requirement)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("libraryimport/service: %w", err)
	}
	var disks int
	if err := queryer.QueryRowContext(ctx, `
SELECT count(*)
FROM dat_disk_entries
WHERE dat_version_id=?
AND machine_name=?
AND COALESCE(status,
'GOOD')!='NODUMP'
`, datID, machine).Scan(&disks); err != nil {
		return nil, false, fmt.Errorf("libraryimport/service: %w", err)
	}
	return requirements, disks > 0, nil
}

func matchArcadeRequirements(
	entries map[string]importing.ArchiveEntry,
	requirements []arcadeROMRequirement,
) ([]string, []string, []string) {
	foldedEntries := make(map[string]importing.ArchiveEntry, len(entries))
	for name, entry := range entries {
		foldedEntries[importing.ASCIICaseFold(name)] = entry
	}
	var missing, mismatched, warnings []string
	for _, requirement := range requirements {
		entry, exists := foldedEntries[importing.ASCIICaseFold(requirement.name)]
		if !exists {
			missing = append(missing, requirement.name)
			continue
		}
		if !arcadeEntryMatchesRequirement(entry, requirement) {
			mismatched = append(mismatched, requirement.name)
		}
		if requirement.status == "BADDUMP" {
			warnings = append(warnings, requirement.name)
		}
	}
	return missing, mismatched, warnings
}

func arcadeEntryMatchesRequirement(entry importing.ArchiveEntry, requirement arcadeROMRequirement) bool {
	return entry.Size == requirement.size &&
		(!requirement.crc32.Valid || strings.EqualFold(entry.CRC32, requirement.crc32.String)) &&
		(!requirement.sha1.Valid || strings.EqualFold(entry.SHA1, requirement.sha1.String))
}

func containsMergedArcadeEntries(entries map[string]importing.ArchiveEntry, requirements []arcadeROMRequirement) bool {
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
		name := importing.ASCIICaseFold(requirement.name)
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

func (service *Service) arcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]string, []string, []arcadeClosureNode, bool, error) {
	nodes, cyclic, err := service.loadArcadeDependencyClosure(ctx, datID, machine)
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
func (service *Service) prepareArcadeFiles(
	ctx context.Context,
	files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	prepared, archives := service.prepareArcadeArchives(ctx, files, datID)
	byMachine := indexedArcadeArchives(prepared)
	dependencyCandidates := service.findUploadedArcadeDependencies(ctx, prepared, byMachine, datID)
	referenced := make(map[string]struct{})
	groups := make([]preparedGroup, 0)
	for index := range prepared {
		primary := &prepared[index]
		if !isPrimaryArcadeArchive(primary, dependencyCandidates) {
			continue
		}
		builder := newArcadeGroupBuilder(ctx, service, datID.String, primary, byMachine, referenced)
		groups = append(groups, builder.build())
	}
	return arcadeDispositions(prepared, referenced), groups, archives
}

func (service *Service) prepareArcadeArchives(
	ctx context.Context,
	files []importSourceFile,
	datID sql.NullString,
) ([]arcadePreparedArchive, []preparedArchive) {
	prepared := make([]arcadePreparedArchive, 0, len(files))
	archives := make([]preparedArchive, 0, len(files))
	for _, file := range files {
		candidate, archive := service.prepareArcadeArchive(ctx, file, datID)
		prepared = append(prepared, candidate)
		if archive != nil {
			archives = append(archives, *archive)
		}
	}
	return prepared, archives
}

func (service *Service) prepareArcadeArchive(
	ctx context.Context,
	file importSourceFile,
	datID sql.NullString,
) (arcadePreparedArchive, *preparedArchive) {
	candidate := arcadePreparedArchive{
		file: file, machine: strings.TrimSuffix(filepath.Base(file.path), filepath.Ext(file.path)),
	}
	if knownSidecar(file.path) {
		candidate.reason = "IGNORED_SYSTEM_SIDECAR"
		return candidate, nil
	}
	if !strings.EqualFold(filepath.Ext(file.path), ".zip") || service.blobs == nil {
		candidate.reason = "UNSUPPORTED_CONTENT_FORMAT"
		return candidate, nil
	}
	entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DefaultArchiveLimits())
	if err != nil {
		candidate.reason = archiveReason(err)
		return candidate, nil
	}
	candidate.entries = entries
	candidate.entryByName = make(map[string]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		candidate.entryByName[entry.NormalizedPath] = entry
	}
	archive := &preparedArchive{blobID: file.blobID, entries: entries}
	if !datID.Valid {
		candidate.reason = "ARCADE_DAT_UNAVAILABLE"
		return candidate, archive
	}
	err = service.database.QueryRowContext(ctx, `
SELECT classification FROM dat_machines WHERE dat_version_id=? AND machine_name=?
`, datID.String, candidate.machine).Scan(&candidate.classification)
	if err != nil {
		candidate.reason = "ARCADE_MACHINE_NOT_FOUND"
	}
	return candidate, archive
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

func (service *Service) findUploadedArcadeDependencies(
	ctx context.Context,
	prepared []arcadePreparedArchive,
	byMachine map[string]*arcadePreparedArchive,
	datID sql.NullString,
) map[string]struct{} {
	result := make(map[string]struct{})
	if !datID.Valid {
		return result
	}
	for index := range prepared {
		candidate := &prepared[index]
		if candidate.reason != "" || candidate.classification != "NORMAL" {
			continue
		}
		parents, bases, _, cyclic, err := service.arcadeDependencyClosure(ctx, datID.String, candidate.machine)
		if err != nil || cyclic {
			continue
		}
		for _, machine := range append(parents, bases...) {
			if _, uploaded := byMachine[machine]; uploaded {
				result[machine] = struct{}{}
			}
		}
	}
	return result
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
	service          *Service
	ctx              context.Context
	datID            string
	primary          *arcadePreparedArchive
	byMachine        map[string]*arcadePreparedArchive
	referenced       map[string]struct{}
	parents          []string
	bases            []string
	closure          []arcadeClosureNode
	closureByMachine map[string]arcadeClosureNode
	status           string
	code             string
	missing          []string
	mismatched       []string
	warnings         []string
	dependencies     []map[string]any
	mergedROMSet     bool
	sources          []preparedSource
	validationFiles  []preparedValidationFile
}

func newArcadeGroupBuilder(
	ctx context.Context,
	service *Service,
	datID string,
	primary *arcadePreparedArchive,
	byMachine map[string]*arcadePreparedArchive,
	referenced map[string]struct{},
) *arcadeGroupBuilder {
	return &arcadeGroupBuilder{
		service: service, ctx: ctx, datID: datID, primary: primary,
		byMachine: byMachine, referenced: referenced, status: "READY", code: "READY",
		missing: make([]string, 0), mismatched: make([]string, 0), warnings: make([]string, 0),
		dependencies: make([]map[string]any, 0), validationFiles: make([]preparedValidationFile, 0),
		sources: []preparedSource{{
			file: primary.file, role: "CONTENT", logicalName: primary.machine + ".zip",
		}},
	}
}

func (builder *arcadeGroupBuilder) build() preparedGroup {
	builder.loadClosure()
	builder.validatePrimary()
	builder.addDependencies(builder.parents, "PARENT", "PARENT")
	builder.addDependencies(builder.bases, "BIOS_OR_BASE", "BIOS_BUNDLE")
	if builder.mergedROMSet {
		builder.status, builder.code = "BLOCKED", "UNSUPPORTED_MERGED_ROMSET"
	}
	return builder.result()
}

func (builder *arcadeGroupBuilder) loadClosure() {
	parents, bases, closure, cyclic, err := builder.service.arcadeDependencyClosure(
		builder.ctx, builder.datID, builder.primary.machine,
	)
	builder.parents, builder.bases, builder.closure = parents, bases, closure
	if err != nil {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
	}
	if cyclic {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DEPENDENCY_CYCLE"
	}
	builder.closureByMachine = make(map[string]arcadeClosureNode, len(closure))
	for _, node := range closure {
		builder.closureByMachine[node.Machine] = node
	}
	if err != nil || cyclic {
		builder.parents, builder.bases = nil, nil
	}
}

func (builder *arcadeGroupBuilder) validatePrimary() {
	requirements, hasDisk, err := builder.service.arcadeRequirements(
		builder.ctx, builder.datID, builder.primary.machine,
	)
	if err != nil {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
	} else if hasDisk {
		builder.status, builder.code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
	}
	direct := directArcadeRequirements(builder.primary.entryByName, requirements)
	missing, mismatched, warnings := matchArcadeRequirements(builder.primary.entryByName, direct)
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
	requirements []arcadeROMRequirement,
) []arcadeROMRequirement {
	result := make([]arcadeROMRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		if !requirement.mergeName.Valid {
			result = append(result, requirement)
		} else if _, included := entries[requirement.name]; included {
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
	requirements, hasDisk, err := builder.service.arcadeRequirements(builder.ctx, builder.datID, name)
	requiredEntries := arcadeRequirementNames(requirements)
	if err != nil {
		builder.status, builder.code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		return
	}
	if hasDisk {
		builder.status, builder.code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
		return
	}
	if containsMergedArcadeEntries(builder.primary.entryByName, requirements) {
		builder.mergedROMSet = true
	}
	contentMissing, contentMismatch, _ := matchArcadeRequirements(builder.primary.entryByName, requirements)
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

func arcadeRequirementNames(requirements []arcadeROMRequirement) []string {
	result := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		result = append(result, requirement.name)
	}
	return result
}

func (builder *arcadeGroupBuilder) recordMissingDependency(
	node arcadeClosureNode,
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
	node arcadeClosureNode,
	name, kind, role string,
	requirements []arcadeROMRequirement,
	requiredEntries []string,
	companion *arcadePreparedArchive,
) {
	missing, mismatched, warnings := matchArcadeRequirements(companion.entryByName, requirements)
	if len(missing) > 0 || len(mismatched) > 0 && kind == "PARENT" {
		builder.missing = append(builder.missing, missing...)
		builder.mismatched = append(builder.mismatched, mismatched...)
		builder.status, builder.code = "BLOCKED", "ARCADE_DEPENDENCY_MISMATCH"
		builder.appendDependency(node, name, kind, "MISMATCH", requiredEntries)
		return
	}
	state := "SATISFIED_EXTERNAL"
	if len(mismatched) > 0 {
		state = "HASH_WARNING"
		builder.warnings = append(builder.warnings, mismatched...)
	}
	builder.warnings = append(builder.warnings, warnings...)
	builder.referenced[companion.file.id] = struct{}{}
	builder.sources = append(builder.sources, preparedSource{
		file: companion.file, role: "COMPANION", logicalName: name + ".zip",
	})
	builder.validationFiles = append(builder.validationFiles, preparedValidationFile{
		role: role, logicalName: name + ".zip", blobID: companion.file.blobID,
		sortOrder: len(builder.validationFiles),
	})
	builder.appendDependency(node, name, kind, state, requiredEntries)
}

func (builder *arcadeGroupBuilder) appendDependency(
	node arcadeClosureNode,
	name, kind, state string,
	requiredEntries []string,
) {
	builder.dependencies = append(builder.dependencies, map[string]any{
		"kind": kind, "machine": name, "requiredBy": node.RequiredBy, "depth": node.Depth,
		"expectedLogicalName": name + ".zip", "state": state,
		"requiredEntryCount": len(requiredEntries), "requiredEntries": requiredEntries,
	})
}

func (builder *arcadeGroupBuilder) result() preparedGroup {
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
	return preparedGroup{
		sources: builder.sources, validationStatus: builder.status, compatibilityCode: builder.code,
		dependencySnapshot: string(snapshot), validationFiles: builder.validationFiles,
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
) []preparedDisposition {
	result := make([]preparedDisposition, 0, len(prepared))
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
			_, used := referenced[candidate.file.id]
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
