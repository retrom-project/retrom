package libraryimport

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentmanifest"
	"retrom/internal/importing"
	"retrom/internal/metadatascrape"
)

var (
	ErrInvalid                        = errors.New("IMPORT_INVALID")
	ErrReimportRequiredPlatformChange = errors.New("REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE")
)

type CreateRequest struct {
	UploadID                 string `json:"uploadId"`
	TargetPlatformInstanceID string `json:"targetPlatformInstanceId"`
	MetadataProvider         string `json:"metadataProvider"`
}

type Created struct {
	ImportJobID string `json:"importJobId"`
	JobID       string `json:"jobId"`
	State       string `json:"state"`
	ItemCount   int    `json:"itemCount"`
}

type importSourceFile struct {
	id, path, blobID, sha256 string
}

type preparedDisposition struct {
	file        importSourceFile
	disposition string
	reason      string
}

type preparedSource struct {
	file           importSourceFile
	role           string
	logicalName    string
	archiveBlobID  string
	archiveOrdinal *int
}

type preparedArchive struct {
	blobID       string
	entries      []importing.ArchiveEntry
	materialized map[int]blobstore.Metadata
}

type preparedGroup struct {
	sources            []preparedSource
	dosEntries         []preparedDOSEntry
	defaultDOSEntry    string
	bundleBlobID       string
	bundle             *blobstore.Metadata
	validationStatus   string
	compatibilityCode  string
	dependencySnapshot string
	titleSource        string
	validationFiles    []preparedValidationFile
}

type preparedValidationFile struct {
	role, logicalName, blobID string
	sortOrder                 int
}

type preparedDOSEntry struct {
	path, kind string
	rank       int
	safe       bool
}

var platformExtensions = map[string]map[string]struct{}{
	"nes":  {".nes": {}, ".unf": {}, ".unif": {}},
	"fds":  {".fds": {}},
	"snes": {".sfc": {}, ".smc": {}, ".swc": {}, ".fig": {}},
	"gbc":  {".gb": {}, ".gbc": {}, ".dmg": {}},
	"gba":  {".gba": {}},
}

func knownSidecar(path string) bool {
	base := filepath.Base(path)
	return base == ".DS_Store" || base == "Thumbs.db" || strings.HasPrefix(base, "._")
}

func archiveReason(err error) string {
	switch {
	case errors.Is(err, importing.ErrArchiveLimitExceeded):
		return "ARCHIVE_LIMIT_EXCEEDED"
	case errors.Is(err, importing.ErrNestedArchiveUnsupported):
		return "NESTED_ARCHIVE_UNSUPPORTED"
	case errors.Is(err, importing.ErrArchiveMethodUnsupported):
		return "ARCHIVE_METHOD_UNSUPPORTED"
	case errors.Is(err, importing.ErrArchiveCasefoldCollision):
		return "ARCHIVE_CASEFOLD_COLLISION"
	default:
		return "ARCHIVE_UNSAFE"
	}
}

func (service *Service) materializeArchiveEntry(
	ctx context.Context,
	archivePath string,
	expected importing.ArchiveEntry,
) (blobstore.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return blobstore.Metadata{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return blobstore.Metadata{}, importing.ErrArchiveUnsafe
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	if expected.Ordinal < 0 || expected.Ordinal >= len(reader.File) {
		return blobstore.Metadata{}, importing.ErrArchiveUnsafe
	}
	entry, err := reader.File[expected.Ordinal].Open()
	if err != nil {
		return blobstore.Metadata{}, importing.ErrArchiveUnsafe
	}
	metadata, putErr := service.blobs.Put(io.LimitReader(entry, expected.Size+1))
	closeErr := entry.Close()
	if putErr != nil || closeErr != nil || metadata.Size != expected.Size || metadata.CRC32 != expected.CRC32 ||
		metadata.MD5 != expected.MD5 ||
		metadata.SHA1 != expected.SHA1 ||
		metadata.SHA256 != expected.SHA256 {
		return blobstore.Metadata{}, importing.ErrArchiveUnsafe
	}
	return metadata, nil
}

func dosProgram(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe":
		return "EXE", true
	case ".com":
		return "COM", true
	case ".bat":
		return "BAT", true
	default:
		return "", false
	}
}

func directDOSPathSafe(path string) bool {
	if path == "" {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if len(segment) == 0 || len(segment) > 255 || !asciiAlphaNumeric(segment[0]) ||
			!asciiDOSPathLast(segment[len(segment)-1]) {
			return false
		}
		for index := 1; index < len(segment)-1; index++ {
			value := segment[index]
			if !asciiAlphaNumeric(value) && value != ' ' && value != '.' && value != '_' && value != '-' {
				return false
			}
		}
	}
	return true
}

func asciiAlphaNumeric(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func asciiDOSPathLast(value byte) bool {
	return asciiAlphaNumeric(value) || value == '_' || value == '-'
}

func dosDirectoryTitle(files []importSourceFile) string {
	if len(files) == 0 {
		return ""
	}
	first := strings.Split(files[0].path, "/")
	commonLength := len(first) - 1
	for _, file := range files[1:] {
		segments := strings.Split(file.path, "/")
		limit := min(commonLength, len(segments)-1)
		commonLength = 0
		for commonLength < limit && first[commonLength] == segments[commonLength] {
			commonLength++
		}
	}
	if commonLength > 0 {
		return first[commonLength-1]
	}
	return strings.TrimSuffix(filepath.Base(files[0].path), filepath.Ext(files[0].path))
}

func (service *Service) bundleDOSDirectory(files []importSourceFile) (blobstore.Metadata, error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		archive := zip.NewWriter(writer)
		var buildErr error
		for _, file := range files {
			header := &zip.FileHeader{Name: file.path, Method: zip.Store}
			header.SetMode(0o644)
			header.Modified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
			destination, err := archive.CreateHeader(header)
			if err != nil {
				buildErr = err
				break
			}
			source, err := service.blobs.OpenDigest(file.sha256)
			if err != nil {
				buildErr = err
				break
			}
			_, copyErr := io.Copy(destination, source)
			cleanup.Error("close", source.Close())
			if copyErr != nil {
				buildErr = copyErr
				break
			}
		}
		closeErr := archive.Close()
		if buildErr == nil {
			buildErr = closeErr
		}
		_ = writer.CloseWithError(buildErr)
		done <- buildErr
	}()
	metadata, err := service.blobs.Put(reader)
	buildErr := <-done
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if buildErr != nil {
		return blobstore.Metadata{}, buildErr
	}
	return metadata, nil
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) prepareDOSFiles(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	dispositions := make([]preparedDisposition, 0, len(files))
	candidates := make([]importSourceFile, 0, len(files))
	for _, file := range files {
		if knownSidecar(file.path) {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "IGNORED", reason: "IGNORED_SYSTEM_SIDECAR"},
			)
			continue
		}
		candidates = append(candidates, file)
	}
	if sourceType == "FILES" {
		if len(candidates) != 1 || !strings.EqualFold(filepath.Ext(candidates[0].path), ".zip") ||
			service.blobs == nil {
			for _, file := range candidates {
				dispositions = append(
					dispositions,
					preparedDisposition{file: file, disposition: "REJECTED", reason: "AMBIGUOUS_DOS_BUNDLE"},
				)
			}
			return dispositions, nil, nil
		}
		file := candidates[0]
		entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DefaultArchiveLimits())
		if err != nil {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: archiveReason(err)},
			)
			return dispositions, nil, nil
		}
		programs := make([]preparedDOSEntry, 0)
		sources := make([]preparedSource, 0, len(entries))
		materialized := make(map[int]blobstore.Metadata, len(entries))
		for _, entry := range entries {
			metadata, materializeErr := service.materializeArchiveEntry(ctx, service.blobs.Path(file.sha256), entry)
			if materializeErr != nil {
				dispositions = append(
					dispositions,
					preparedDisposition{file: file, disposition: "REJECTED", reason: archiveReason(materializeErr)},
				)
				return dispositions, nil, nil
			}
			ordinal := entry.Ordinal
			materialized[ordinal] = metadata
			sources = append(
				sources,
				preparedSource{
					file:           file,
					role:           "DOS_SOURCE",
					logicalName:    entry.NormalizedPath,
					archiveBlobID:  file.blobID,
					archiveOrdinal: &ordinal,
				},
			)
			if kind, ok := dosProgram(entry.NormalizedPath); ok {
				programs = append(
					programs,
					preparedDOSEntry{
						path: entry.NormalizedPath,
						kind: kind,
						rank: len(programs),
						safe: directDOSPathSafe(entry.NormalizedPath),
					},
				)
			}
		}
		if len(programs) == 0 {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "NO_DOS_PROGRAM"},
			)
			return dispositions, nil, nil
		}
		dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
		return dispositions, []preparedGroup{
				{
					sources:         sources,
					dosEntries:      programs,
					defaultDOSEntry: programs[0].path,
					bundleBlobID:    file.blobID,
					titleSource:     filepath.Base(file.path),
				},
			}, []preparedArchive{
				{blobID: file.blobID, entries: entries, materialized: materialized},
			}
	}
	if len(candidates) == 0 || service.blobs == nil {
		return dispositions, nil, nil
	}
	programs := make([]preparedDOSEntry, 0)
	sources := make([]preparedSource, 0, len(candidates))
	for _, file := range candidates {
		dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
		sources = append(sources, preparedSource{file: file, role: "DOS_SOURCE", logicalName: file.path})
		if kind, ok := dosProgram(file.path); ok {
			programs = append(
				programs,
				preparedDOSEntry{path: file.path, kind: kind, rank: len(programs), safe: directDOSPathSafe(file.path)},
			)
		}
	}
	if len(programs) == 0 {
		for index := range dispositions {
			if dispositions[index].disposition == "SOURCE" {
				dispositions[index].disposition = "REJECTED"
				dispositions[index].reason = "NO_DOS_PROGRAM"
			}
		}
		return dispositions, nil, nil
	}
	bundle, err := service.bundleDOSDirectory(candidates)
	if err != nil {
		for index := range dispositions {
			if dispositions[index].disposition == "SOURCE" {
				dispositions[index].disposition = "REJECTED"
				dispositions[index].reason = "DOS_BUNDLE_FAILED"
			}
		}
		return dispositions, nil, nil
	}
	return dispositions, []preparedGroup{
		{
			sources:         sources,
			dosEntries:      programs,
			defaultDOSEntry: programs[0].path,
			bundle:          &bundle,
			titleSource:     dosDirectoryTitle(candidates),
		},
	}, nil
}

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
	var defaultBIOS sql.NullString
	_ = service.database.QueryRowContext(ctx, `
SELECT bios_name
FROM dat_bios_sets
WHERE dat_version_id=?
AND machine_name=?
AND is_default=1
`, datID, machine).
		Scan(&defaultBIOS)
	rows, err := service.database.QueryContext(
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
	if err := service.database.QueryRowContext(ctx, `
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
	var missing, mismatched, warnings []string
	for _, requirement := range requirements {
		entry, exists := entries[requirement.name]
		if !exists {
			missing = append(missing, requirement.name)
			continue
		}
		if entry.Size != requirement.size ||
			requirement.crc32.Valid && !strings.EqualFold(entry.CRC32, requirement.crc32.String) ||
			requirement.sha1.Valid && !strings.EqualFold(entry.SHA1, requirement.sha1.String) {
			mismatched = append(mismatched, requirement.name)
		}
		if requirement.status == "BADDUMP" {
			warnings = append(warnings, requirement.name)
		}
	}
	return missing, mismatched, warnings
}

func containsMergedArcadeEntries(entries map[string]importing.ArchiveEntry, requirements []arcadeROMRequirement) bool {
	requiredNames := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		requiredNames[requirement.name] = struct{}{}
	}
	for path := range entries {
		if !strings.Contains(path, "/") {
			continue
		}
		if _, required := requiredNames[filepath.Base(path)]; required {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (service *Service) arcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]string, []string, []string, bool, error) {
	var parents, bases, all []string
	current := machine
	seen := map[string]struct{}{}
	for current != "" {
		if _, exists := seen[current]; exists || len(seen) >= 64 {
			return nil, nil, nil, true, nil
		}
		seen[current] = struct{}{}
		all = append(all, current)
		var cloneOf, romOf sql.NullString
		if err := service.database.QueryRowContext(ctx, `
SELECT cloneof,
romof
FROM dat_machines
WHERE dat_version_id=?
AND machine_name=?
`, datID, current).Scan(&cloneOf, &romOf); err != nil {
			return nil, nil, nil, false, fmt.Errorf("libraryimport/service: %w", err)
		}
		if romOf.Valid && (!cloneOf.Valid || romOf.String != cloneOf.String) {
			bases = appendUnique(bases, romOf.String)
			all = appendUnique(all, romOf.String)
		}
		if !cloneOf.Valid {
			break
		}
		parents = appendUnique(parents, cloneOf.String)
		current = cloneOf.String
	}
	return parents, bases, all, false, nil
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) prepareArcadeFiles(
	ctx context.Context,
	files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	prepared := make([]arcadePreparedArchive, 0, len(files))
	archives := make([]preparedArchive, 0, len(files))
	for _, file := range files {
		candidate := arcadePreparedArchive{
			file:    file,
			machine: strings.TrimSuffix(filepath.Base(file.path), filepath.Ext(file.path)),
		}
		if knownSidecar(file.path) {
			candidate.reason = "IGNORED_SYSTEM_SIDECAR"
			prepared = append(prepared, candidate)
			continue
		}
		if !strings.EqualFold(filepath.Ext(file.path), ".zip") || service.blobs == nil {
			candidate.reason = "UNSUPPORTED_CONTENT_FORMAT"
			prepared = append(prepared, candidate)
			continue
		}
		entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DefaultArchiveLimits())
		if err != nil {
			candidate.reason = archiveReason(err)
			prepared = append(prepared, candidate)
			continue
		}
		candidate.entries = entries
		candidate.entryByName = make(map[string]importing.ArchiveEntry, len(entries))
		for _, entry := range entries {
			candidate.entryByName[entry.NormalizedPath] = entry
		}
		archives = append(archives, preparedArchive{blobID: file.blobID, entries: entries})
		if !datID.Valid {
			candidate.reason = "ARCADE_DAT_UNAVAILABLE"
		} else if err := service.database.QueryRowContext(ctx, `
SELECT classification
FROM dat_machines
WHERE dat_version_id=?
AND machine_name=?
`, datID.String, candidate.machine).Scan(&candidate.classification); err != nil {
			candidate.reason = "ARCADE_MACHINE_NOT_FOUND"
		}
		prepared = append(prepared, candidate)
	}
	byMachine := make(map[string]*arcadePreparedArchive, len(prepared))
	for index := range prepared {
		if prepared[index].classification != "" {
			byMachine[prepared[index].machine] = &prepared[index]
		}
	}
	referenced := make(map[string]struct{})
	groups := make([]preparedGroup, 0)
	for index := range prepared {
		primary := &prepared[index]
		if primary.reason != "" || primary.classification != "NORMAL" {
			continue
		}
		parents, bases, closure, cyclic, err := service.arcadeDependencyClosure(ctx, datID.String, primary.machine)
		status, code := "READY", "READY"
		missing := make([]string, 0)
		mismatched := make([]string, 0)
		warnings := make([]string, 0)
		dependencies := make([]map[string]any, 0)
		mergedROMSet := false
		if err != nil {
			status, code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		}
		if cyclic {
			status, code = "INCOMPATIBLE", "ARCADE_DEPENDENCY_CYCLE"
		}
		primaryRequirements, hasDisk, requirementErr := service.arcadeRequirements(ctx, datID.String, primary.machine)
		if requirementErr != nil {
			status, code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		} else if hasDisk {
			status, code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
		}
		primaryDirectRequirements := make([]arcadeROMRequirement, 0, len(primaryRequirements))
		for _, requirement := range primaryRequirements {
			if !requirement.mergeName.Valid {
				primaryDirectRequirements = append(primaryDirectRequirements, requirement)
			} else if _, included := primary.entryByName[requirement.name]; included {
				primaryDirectRequirements = append(primaryDirectRequirements, requirement)
			}
		}
		primaryMissing, primaryMismatch, primaryWarnings := matchArcadeRequirements(
			primary.entryByName,
			primaryDirectRequirements,
		)
		mergedROMSet = containsMergedArcadeEntries(primary.entryByName, primaryRequirements)
		missing = append(missing, primaryMissing...)
		mismatched = append(mismatched, primaryMismatch...)
		warnings = append(warnings, primaryWarnings...)
		if len(primaryMissing) > 0 || len(primaryMismatch) > 0 {
			status, code = "BLOCKED", "ARCADE_CONTENT_MISSING_ENTRY"
		}
		sources := []preparedSource{{file: primary.file, role: "CONTENT", logicalName: primary.machine + ".zip"}}
		validationFiles := make([]preparedValidationFile, 0)
		addDependencies := func(names []string, kind, role string) {
			for _, name := range names {
				requirements, dependencyHasDisk, loadErr := service.arcadeRequirements(ctx, datID.String, name)
				requiredEntries := make([]string, 0, len(requirements))
				for _, requirement := range requirements {
					requiredEntries = append(requiredEntries, requirement.name)
				}
				if loadErr != nil {
					status, code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
					continue
				}
				if dependencyHasDisk {
					status, code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
					continue
				}
				if containsMergedArcadeEntries(primary.entryByName, requirements) {
					mergedROMSet = true
				}
				contentMissing, contentMismatch, _ := matchArcadeRequirements(primary.entryByName, requirements)
				if len(contentMissing) == 0 && len(contentMismatch) == 0 {
					dependencies = append(
						dependencies,
						map[string]any{
							"kind":            kind,
							"machine":         name,
							"state":           "SATISFIED_BY_CONTENT",
							"requiredEntries": requiredEntries,
						},
					)
					continue
				}
				companion := byMachine[name]
				if companion == nil || companion.reason != "" {
					missing = append(missing, name+".zip")
					if status == "READY" {
						status = "BLOCKED"
						if kind == "PARENT" {
							code = "LAUNCH_PARENT_MISSING"
						} else {
							code = "LAUNCH_BIOS_MISSING"
						}
					}
					dependencies = append(
						dependencies,
						map[string]any{
							"kind":            kind,
							"machine":         name,
							"state":           "MISSING",
							"requiredEntries": requiredEntries,
						},
					)
					continue
				}
				dependencyMissing, dependencyMismatch, dependencyWarnings := matchArcadeRequirements(
					companion.entryByName,
					requirements,
				)
				if len(dependencyMissing) > 0 || len(dependencyMismatch) > 0 && kind == "PARENT" {
					missing = append(missing, dependencyMissing...)
					mismatched = append(mismatched, dependencyMismatch...)
					status, code = "BLOCKED", "ARCADE_DEPENDENCY_MISMATCH"
					dependencies = append(
						dependencies,
						map[string]any{
							"kind":            kind,
							"machine":         name,
							"state":           "MISMATCH",
							"requiredEntries": requiredEntries,
						},
					)
					continue
				}
				dependencyState := "SATISFIED_EXTERNAL"
				if len(dependencyMismatch) > 0 {
					dependencyState = "HASH_WARNING"
					warnings = append(warnings, dependencyMismatch...)
				}
				warnings = append(warnings, dependencyWarnings...)
				referenced[companion.file.id] = struct{}{}
				sources = append(
					sources,
					preparedSource{file: companion.file, role: "COMPANION", logicalName: name + ".zip"},
				)
				validationFiles = append(
					validationFiles,
					preparedValidationFile{
						role:        role,
						logicalName: name + ".zip",
						blobID:      companion.file.blobID,
						sortOrder:   len(validationFiles),
					},
				)
				dependencies = append(
					dependencies,
					map[string]any{
						"kind":            kind,
						"machine":         name,
						"state":           dependencyState,
						"requiredEntries": requiredEntries,
					},
				)
			}
		}
		if !cyclic && err == nil {
			addDependencies(parents, "PARENT", "PARENT")
			addDependencies(bases, "BIOS_OR_BASE", "BIOS_BUNDLE")
		}
		if mergedROMSet {
			status, code = "BLOCKED", "UNSUPPORTED_MERGED_ROMSET"
		}
		sort.Strings(missing)
		sort.Strings(mismatched)
		sort.Strings(warnings)
		snapshot, _ := json.Marshal(
			map[string]any{
				"schemaVersion":     1,
				"machine":           primary.machine,
				"datVersionId":      datID.String,
				"closure":           closure,
				"dependencies":      dependencies,
				"missingEntries":    missing,
				"mismatchedEntries": mismatched,
				"warnings":          warnings,
			},
		)
		groups = append(
			groups,
			preparedGroup{
				sources:            sources,
				validationStatus:   status,
				compatibilityCode:  code,
				dependencySnapshot: string(snapshot),
				validationFiles:    validationFiles,
			},
		)
	}
	dispositions := make([]preparedDisposition, 0, len(prepared))
	for index := range prepared {
		candidate := &prepared[index]
		switch {
		case candidate.reason == "IGNORED_SYSTEM_SIDECAR":
			dispositions = append(
				dispositions,
				preparedDisposition{file: candidate.file, disposition: "IGNORED", reason: candidate.reason},
			)
		case candidate.reason != "":
			dispositions = append(
				dispositions,
				preparedDisposition{file: candidate.file, disposition: "REJECTED", reason: candidate.reason},
			)
		case candidate.classification == "NORMAL":
			dispositions = append(dispositions, preparedDisposition{file: candidate.file, disposition: "SOURCE"})
		default:
			if _, used := referenced[candidate.file.id]; used {
				dispositions = append(dispositions, preparedDisposition{file: candidate.file, disposition: "SOURCE"})
			} else {
				dispositions = append(dispositions, preparedDisposition{
					file: candidate.file, disposition: "REJECTED", reason: "ARCADE_UNUSED_DEPENDENCY_ARCHIVE",
				})
			}
		}
	}
	return dispositions, groups, archives
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) prepareImportFiles(
	ctx context.Context,
	platformID, sourceType string,
	files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	if platformID == "dos" {
		return service.prepareDOSFiles(ctx, sourceType, files)
	}
	dispositions := make([]preparedDisposition, 0, len(files))
	groups := make([]preparedGroup, 0, len(files))
	archives := make([]preparedArchive, 0)
	if platformID == "arcade" {
		return service.prepareArcadeFiles(ctx, files, datID)
	}
	extensions := platformExtensions[platformID]
	if extensions == nil {
		for _, file := range files {
			if knownSidecar(file.path) {
				dispositions = append(
					dispositions,
					preparedDisposition{file: file, disposition: "IGNORED", reason: "IGNORED_SYSTEM_SIDECAR"},
				)
				continue
			}
			extension := strings.ToLower(filepath.Ext(file.path))
			allowed := platformID == "arcade" && extension == ".zip" ||
				platformID == "dos" && (sourceType == "DIRECTORY" || extension == ".zip")
			if !allowed {
				dispositions = append(
					dispositions,
					preparedDisposition{file: file, disposition: "REJECTED", reason: "UNSUPPORTED_CONTENT_FORMAT"},
				)
				continue
			}
			dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
			groups = append(
				groups,
				preparedGroup{
					sources: []preparedSource{{file: file, role: "CONTENT", logicalName: filepath.Base(file.path)}},
				},
			)
		}
		return dispositions, groups, archives
	}
	for _, file := range files {
		if knownSidecar(file.path) {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "IGNORED", reason: "IGNORED_SYSTEM_SIDECAR"},
			)
			continue
		}
		extension := strings.ToLower(filepath.Ext(file.path))
		if _, supported := extensions[extension]; supported {
			dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
			groups = append(
				groups,
				preparedGroup{
					sources: []preparedSource{{file: file, role: "CONTENT", logicalName: filepath.Base(file.path)}},
				},
			)
			continue
		}
		if extension != ".zip" || service.blobs == nil {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "UNSUPPORTED_CONTENT_FORMAT"},
			)
			continue
		}
		entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DefaultArchiveLimits())
		if err != nil {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: archiveReason(err)},
			)
			continue
		}
		candidates := make([]importing.ArchiveEntry, 0, 2)
		for _, entry := range entries {
			if _, supported := extensions[strings.ToLower(filepath.Ext(entry.NormalizedPath))]; supported {
				candidates = append(candidates, entry)
			}
		}
		if len(candidates) == 0 {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "NO_SUPPORTED_CONTENT"},
			)
			continue
		}
		if len(candidates) != 1 {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "AMBIGUOUS_PRIMARY_CONTENT"},
			)
			continue
		}
		selected, err := service.materializeArchiveEntry(ctx, service.blobs.Path(file.sha256), candidates[0])
		if err != nil {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: archiveReason(err)},
			)
			continue
		}
		ordinal := candidates[0].Ordinal
		dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
		groups = append(
			groups,
			preparedGroup{
				sources: []preparedSource{
					{
						file:           file,
						role:           "CONTENT",
						logicalName:    filepath.Base(candidates[0].NormalizedPath),
						archiveBlobID:  file.blobID,
						archiveOrdinal: &ordinal,
					},
				},
			},
		)
		archives = append(
			archives,
			preparedArchive{
				blobID:       file.blobID,
				entries:      entries,
				materialized: map[int]blobstore.Metadata{ordinal: selected},
			},
		)
	}
	return dispositions, groups, archives
}

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	now      func() time.Time
	scraper  *metadatascrape.Service
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func New(database *sql.DB, now func() time.Time, scraper ...*metadatascrape.Service) *Service {
	service := &Service{database: database, now: now}
	if len(scraper) > 0 {
		service.scraper = scraper[0]
	}
	return service
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Created, error) {
	if request.MetadataProvider != "NONE" && request.MetadataProvider != "HASHEOUS" {
		return Created{}, ErrInvalid
	}
	var uploadState, sourceType string
	if err := service.database.QueryRowContext(ctx, `
SELECT state,
source_type
FROM upload_sessions
WHERE id=?
`, request.UploadID).Scan(&uploadState, &sourceType); err != nil ||
		uploadState != "COMPLETE" {
		return Created{}, ErrInvalid
	}
	var platformID, coreID, artifactID, emulatorVersion, artifactPath, artifactSHA string
	var instanceVersion int64
	if err := service.database.QueryRowContext(ctx, `
SELECT pi.platform_id,
pi.default_core_id,
pi.version,
a.id,
a.emulatorjs_version,
a.relative_path,
a.sha256
FROM platform_instances pi
JOIN core_artifacts a ON a.core_id=pi.default_core_id
AND a.enabled=1
WHERE pi.id=?
AND pi.enabled=1
AND pi.deleted_at_ms IS NULL
`, request.TargetPlatformInstanceID).Scan(
		&platformID,
		&coreID,
		&instanceVersion,
		&artifactID,
		&emulatorVersion,
		&artifactPath,
		&artifactSHA,
	); err != nil {
		return Created{}, ErrInvalid
	}
	var datID sql.NullString
	_ = service.database.QueryRowContext(ctx, `
SELECT id
FROM dat_versions
WHERE core_artifact_id=?
AND is_active=1
`, artifactID).
		Scan(&datID)
	fileRows, err := service.database.QueryContext(
		ctx,
		`
SELECT f.id,
f.relative_path,
f.final_blob_id,
b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.upload_session_id=?
AND f.state='COMPLETE'
ORDER BY f.relative_path,
f.id
`,
		request.UploadID,
	)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", fileRows.Close()) }()
	var files []importSourceFile
	for fileRows.Next() {
		var file importSourceFile
		if err := fileRows.Scan(&file.id, &file.path, &file.blobID, &file.sha256); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		files = append(files, file)
	}
	if err := fileRows.Err(); err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if len(files) == 0 {
		return Created{}, ErrInvalid
	}
	dispositions, groups, archives := service.prepareImportFiles(ctx, platformID, sourceType, files, datID)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var lockedUploadState string
	if err := transaction.QueryRowContext(ctx, `
SELECT state
FROM upload_sessions
WHERE id=?
`, request.UploadID).Scan(&lockedUploadState); err != nil ||
		lockedUploadState != "COMPLETE" {
		return Created{}, ErrInvalid
	}
	var lockedVersion int64
	var lockedArtifactID string
	if err := transaction.QueryRowContext(ctx, `
SELECT pi.version,
a.id
FROM platform_instances pi
JOIN core_artifacts a ON a.core_id=pi.default_core_id
AND a.enabled=1
WHERE pi.id=?
AND pi.enabled=1
AND pi.deleted_at_ms IS NULL
`, request.TargetPlatformInstanceID).Scan(&lockedVersion, &lockedArtifactID); err != nil ||
		lockedVersion != instanceVersion ||
		lockedArtifactID != artifactID {
		return Created{}, ErrInvalid
	}
	importID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	consumptionID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	config := map[string]any{
		"platformInstanceId":            request.TargetPlatformInstanceID,
		"platformInstanceVersion":       instanceVersion,
		"platformId":                    platformID,
		"defaultCoreId":                 coreID,
		"coreArtifactId":                artifactID,
		"emulatorjsVersion":             emulatorVersion,
		"coreArtifactPath":              artifactPath,
		"coreArtifactSha256":            artifactSHA,
		"datVersionId":                  nullable(datID),
		"metadataProviderConfigVersion": 1,
	}
	configJSON, _ := json.Marshal(config)
	configDigest := sha256.Sum256(configJSON)
	scheduledRuns := make([]metadatascrape.Scheduled, 0, len(files))
	jobDedupe := sha256.Sum256([]byte("import:" + request.UploadID))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
execution_started_at_ms,
finished_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'IMPORT_GROUP',
?,
'IMPORT_GROUP',
?,
1,
'{}',
1,
'SUCCEEDED',
1,
2,
?,
?,
?,
?,
?)
`, jobID.String(), importID.String(), hex.EncodeToString(jobDedupe[:]), now, now, now, now, now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
1,
?,
?,
?)
`, jobID.String(), string(configJSON), hex.EncodeToString(configDigest[:]), now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	ignored, rejected := 0, 0
	for _, disposition := range dispositions {
		if disposition.disposition == "IGNORED" {
			ignored++
		}
		if disposition.disposition == "REJECTED" {
			rejected++
		}
	}
	state := "REVIEW_PENDING"
	if rejected > 0 {
		state = "PARTIAL_FAILURE"
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_jobs(id,
upload_session_id,
target_platform_instance_id,
platform_instance_version,
platform_id,
default_core_id,
core_artifact_id,
dat_version_id,
metadata_provider,
config_snapshot_json,
config_snapshot_digest,
state,
total_item_count,
review_pending_item_count,
ignored_file_count,
rejected_file_count,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
1,
?,
?)
`,
		importID.String(),
		request.UploadID,
		request.TargetPlatformInstanceID,
		instanceVersion,
		platformID,
		coreID,
		artifactID,
		nullable(datID),
		request.MetadataProvider,
		string(configJSON),
		hex.EncodeToString(configDigest[:]),
		state,
		len(groups),
		len(groups),
		ignored,
		rejected,
		now,
		now,
	); err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,
?,
NULL,
'IMPORT_JOB',
?,
?)
`, consumptionID.String(), request.UploadID, importID.String(), now); err != nil {
		return Created{}, ErrInvalid
	}
	for _, disposition := range dispositions {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_job_files(import_job_id,
upload_file_id,
disposition,
reason_code,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?)
`,
			importID.String(),
			disposition.file.id,
			disposition.disposition,
			nullableText(disposition.reason),
			now,
			now,
		); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	materialized := make(map[string]string, len(archives))
	for _, archive := range archives {
		materializedBlobIDs := make(map[int]string, len(archive.materialized))
		for ordinal, metadata := range archive.materialized {
			materializedBlobID, registerErr := blobstore.EnsureRecord(
				ctx,
				transaction,
				metadata,
				"application/octet-stream",
				now,
			)
			if registerErr != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", registerErr)
			}
			materializedBlobIDs[ordinal] = materializedBlobID
			materialized[fmt.Sprintf("%s:%d", archive.blobID, ordinal)] = materializedBlobID
		}
		for _, entry := range archive.entries {
			materializedBlobID := nullableText(materializedBlobIDs[entry.Ordinal])
			if _, err := transaction.ExecContext(ctx, `
INSERT
OR IGNORE INTO archive_entries(archive_blob_id,
ordinal,
original_relative_path,
normalized_path,
ascii_casefold_path,
compression_method,
uncompressed_size_bytes,
crc32,
md5,
sha1,
sha256,
materialized_blob_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
				archive.blobID,
				entry.Ordinal,
				entry.OriginalPath,
				entry.NormalizedPath,
				entry.ASCIICasefoldPath,
				entry.Method,
				entry.Size,
				entry.CRC32,
				entry.MD5,
				entry.SHA1,
				entry.SHA256,
				materializedBlobID,
				now,
			); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
			if materializedBlobIDs[entry.Ordinal] != "" {
				if _, err := transaction.ExecContext(ctx, `
UPDATE archive_entries
SET materialized_blob_id=?
WHERE archive_blob_id=?
AND ordinal=?
AND materialized_blob_id IS NULL
`, materializedBlobIDs[entry.Ordinal], archive.blobID, entry.Ordinal); err != nil {
					return Created{}, fmt.Errorf("libraryimport/service: %w", err)
				}
			}
		}
	}
	for _, group := range groups {
		itemID, _ := uuid.NewV7()
		validationID, _ := uuid.NewV7()
		draftID, _ := uuid.NewV7()
		manifestFiles := make([]contentmanifest.File, 0, len(group.sources))
		for _, source := range group.sources {
			blobID := source.file.blobID
			if source.archiveOrdinal != nil {
				blobID = materialized[fmt.Sprintf("%s:%d", source.archiveBlobID, *source.archiveOrdinal)]
			}
			var blobSHA string
			var blobSize int64
			if err := transaction.QueryRowContext(ctx, `
SELECT sha256,
size_bytes
FROM blobs
WHERE id=?
`, blobID).Scan(&blobSHA, &blobSize); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
			var archiveSHA *string
			if source.archiveOrdinal != nil {
				var value string
				if err := transaction.QueryRowContext(ctx, `
SELECT sha256
FROM blobs
WHERE id=?
`, source.archiveBlobID).Scan(&value); err != nil {
					return Created{}, fmt.Errorf("libraryimport/service: %w", err)
				}
				archiveSHA = &value
			}
			manifestFiles = append(
				manifestFiles,
				contentmanifest.File{
					Role:                      source.role,
					LogicalName:               source.logicalName,
					BlobSHA256:                blobSHA,
					SizeBytes:                 blobSize,
					SourceArchiveSHA256:       archiveSHA,
					SourceArchiveEntryOrdinal: source.archiveOrdinal,
				},
			)
		}
		manifestJSON, manifestDigestHex, err := contentmanifest.Build(manifestFiles)
		if err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		manifestDigest := sha256.Sum256(manifestJSON)
		groupIdentity := make([]map[string]any, 0, len(group.sources))
		searchParts := make([]string, 0, len(group.sources))
		for _, source := range group.sources {
			groupIdentity = append(
				groupIdentity,
				map[string]any{
					"relativePath":   source.file.path,
					"sourceSha256":   source.file.sha256,
					"role":           source.role,
					"logicalName":    source.logicalName,
					"archiveOrdinal": nullableIntPointer(source.archiveOrdinal),
				},
			)
			searchParts = append(searchParts, source.file.path)
		}
		groupInput, _ := json.Marshal(groupIdentity)
		groupDigest := sha256.Sum256(groupInput)
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_items(id,
import_job_id,
group_key,
state,
source_manifest_json,
source_manifest_digest,
search_text,
version,
created_at_ms,
updated_at_ms) VALUES(?,
 ?,
 ?,
 'REVIEW_PENDING',
 ?,
 ?,
 ?,
 1,
 ?,
 ?)
`,
			itemID.String(),
			importID.String(),
			hex.EncodeToString(groupDigest[:]),
			string(manifestJSON),
			manifestDigestHex,
			strings.ToLower(strings.Join(searchParts, " ")),
			now,
			now,
		); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		for sortOrder, source := range group.sources {
			blobID := source.file.blobID
			if source.archiveOrdinal != nil {
				blobID = materialized[fmt.Sprintf("%s:%d", source.archiveBlobID, *source.archiveOrdinal)]
			}
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_files(import_item_id,
role,
logical_name,
upload_file_id,
blob_id,
source_archive_blob_id,
source_archive_entry_ordinal,
sort_order,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
				itemID.String(),
				source.role,
				source.logicalName,
				source.file.id,
				blobID,
				nullableText(source.archiveBlobID),
				nullableIntPointer(source.archiveOrdinal),
				sortOrder,
				now,
			); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
		}
		inputDigest := sha256.Sum256(append(manifestDigest[:], configDigest[:]...))
		validationStatus := group.validationStatus
		compatibilityCode := group.compatibilityCode
		dependencySnapshot := group.dependencySnapshot
		if validationStatus == "" {
			validationStatus, compatibilityCode, dependencySnapshot = "READY", "READY", "{}"
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,
import_item_id,
target_platform_instance_id,
platform_instance_version,
core_id,
core_artifact_id,
dat_version_id,
default_dos_entry,
source_manifest_digest,
prepublish_input_digest,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
			validationID.String(),
			itemID.String(),
			request.TargetPlatformInstanceID,
			instanceVersion,
			coreID,
			artifactID,
			nullable(datID),
			nullableText(group.defaultDOSEntry),
			manifestDigestHex,
			hex.EncodeToString(inputDigest[:]),
			validationStatus,
			compatibilityCode,
			dependencySnapshot,
			now,
		); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		for _, dosEntry := range group.dosEntries {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_dos_entries(import_item_id,
normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe,
created_at_ms) VALUES(?,
?,
?,
?,
?,
1,
?,
?)
`, itemID.String(), dosEntry.path, dosEntry.path, dosEntry.kind, dosEntry.rank, dosEntry.safe, now); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
		}
		bundleBlobID := group.bundleBlobID
		if group.bundle != nil {
			bundleBlobID, err = blobstore.EnsureRecord(ctx, transaction, *group.bundle, "application/zip", now)
			if err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
		}
		if bundleBlobID != "" {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,
role,
logical_name,
blob_id,
sort_order,
created_at_ms) VALUES(?,
'DOS_LAUNCH_BUNDLE',
'game.zip',
?,
0,
?)
`, validationID.String(), bundleBlobID, now); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
		}
		for _, file := range group.validationFiles {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,
role,
logical_name,
blob_id,
sort_order,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?)
`, validationID.String(), file.role, file.logicalName, file.blobID, file.sortOrder, now); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
		}
		titleSource := group.titleSource
		if titleSource == "" {
			titleSource = group.sources[0].logicalName
		}
		title := strings.TrimSuffix(filepath.Base(titleSource), filepath.Ext(titleSource))
		metadataJSON, _ := json.Marshal(
			map[string]any{
				"title":       title,
				"description": "",
				"developer":   "",
				"publisher":   "",
				"genre":       "",
				"players":     nil,
				"releaseYear": nil,
			},
		)
		searchText := strings.ToLower(strings.Join(append([]string{itemID.String(), title}, searchParts...), " "))
		if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET search_text=?
WHERE id=?
`, searchText, itemID.String()); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		var selectedValidation any
		if validationStatus == "READY" {
			selectedValidation = validationID.String()
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_drafts(id,
import_item_id,
target_platform_instance_id,
selected_validation_id,
default_dos_entry,
metadata_json,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
1,
?,
?)
`,
			draftID.String(),
			itemID.String(),
			request.TargetPlatformInstanceID,
			selectedValidation,
			nullableText(group.defaultDOSEntry),
			string(metadataJSON),
			now,
			now,
		); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		if service.scraper != nil {
			scheduled, scheduleErr := service.scraper.ScheduleImport(
				ctx,
				transaction,
				itemID.String(),
				request.MetadataProvider,
			)
			if scheduleErr != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", scheduleErr)
			}
			scheduledRuns = append(scheduledRuns, scheduled)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'IMPORT_GROUP',
?,
'SUCCEEDED',
?,
?)
`, jobID.String(), importID.String(), fmt.Sprintf(`{"itemCount":%d}`, len(files)), now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	for _, scheduled := range scheduledRuns {
		if scheduled.IsNoop() {
			continue
		}
		runID := scheduled.ScrapeRunID()
		go func() { _ = service.scraper.Run(context.WithoutCancel(ctx), runID) }()
	}
	return Created{ImportJobID: importID.String(), JobID: jobID.String(), State: state, ItemCount: len(groups)}, nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableIntPointer(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

type Approved struct {
	GameID  string `json:"gameId"`
	EventID string `json:"reviewEventId"`
	Status  string `json:"status"`
}

func (service *Service) Approve(ctx context.Context, itemID string, expectedVersion int64) (Approved, error) {
	return service.ApproveWithReason(ctx, itemID, expectedVersion, nil)
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) ApproveWithReason(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	reason *string,
) (Approved, error) {
	var decisionReason any
	if reason != nil {
		trimmed := strings.TrimSpace(*reason)
		if trimmed == "" || !validField(trimmed, 500, true) {
			return Approved{}, ErrInvalid
		}
		decisionReason = trimmed
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
PRAGMA defer_foreign_keys=ON
`); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	var state, importID, configSnapshotJSON, platformInstanceID, validationID string
	var metadataJSON, sourceManifestJSON, sourceManifestDigest, dependencySnapshotJSON string
	var coreID, artifactID string
	var draftVersion int64
	var datID, validationDOSEntry, draftDOSEntry, candidateID, coverID, backgroundID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT i.state,
i.import_job_id,
j.config_snapshot_json,
d.target_platform_instance_id,
d.selected_validation_id,
d.metadata_json,
i.source_manifest_json,
i.source_manifest_digest,
v.core_id,
v.core_artifact_id,
v.dat_version_id,
v.default_dos_entry,
d.default_dos_entry,
v.dependency_snapshot_json,
d.version,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.background_candidate_asset_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_core_validations v ON v.id=d.selected_validation_id
JOIN platform_instances p ON p.id=d.target_platform_instance_id
AND p.enabled=1
AND p.deleted_at_ms IS NULL
AND p.version=v.platform_instance_version
JOIN core_artifacts a ON a.id=v.core_artifact_id
AND a.core_id=p.default_core_id
AND a.enabled=1
WHERE i.id=?
AND v.status='READY'
AND v.default_dos_entry IS d.default_dos_entry
AND v.dat_version_id IS (SELECT active.id
FROM dat_versions active
WHERE active.core_artifact_id=a.id
AND active.is_active=1)
`, itemID).
		Scan(
			&state,
			&importID,
			&configSnapshotJSON,
			&platformInstanceID,
			&validationID,
			&metadataJSON,
			&sourceManifestJSON,
			&sourceManifestDigest,
			&coreID,
			&artifactID,
			&datID,
			&validationDOSEntry,
			&draftDOSEntry,
			&dependencySnapshotJSON,
			&draftVersion,
			&candidateID,
			&coverID,
			&backgroundID,
		)
	if err != nil || state != "REVIEW_PENDING" || draftVersion != expectedVersion {
		return Approved{}, ErrInvalid
	}
	var metadata struct {
		Title, Description, Developer, Publisher, Genre string
		Players, ReleaseYear                            *int
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil || strings.TrimSpace(metadata.Title) == "" {
		return Approved{}, ErrInvalid
	}
	gameID, _ := uuid.NewV7()
	metadataID, _ := uuid.NewV7()
	contentID, _ := uuid.NewV7()
	variantID, _ := uuid.NewV7()
	variantRevisionID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_metadata_revisions(id,
game_id,
title,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'IMPORT_REVIEW',
?,
?)
`,
		metadataID.String(),
		gameID.String(),
		strings.TrimSpace(metadata.Title),
		metadata.Description,
		metadata.Developer,
		metadata.Publisher,
		metadata.Genre,
		metadata.Players,
		metadata.ReleaseYear,
		itemID,
		now,
	); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_revisions(id,
game_id,
source_kind,
source_ref_id,
source_manifest_json,
source_manifest_digest,
created_at_ms) VALUES(?,
?,
'IMPORT_REVIEW',
?,
?,
?,
?)
`, contentID.String(), gameID.String(), itemID, sourceManifestJSON, sourceManifestDigest, now); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO games(id,
platform_instance_id,
status,
current_metadata_revision_id,
current_content_revision_id,
search_text,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
'PUBLISHED',
?,
?,
?,
1,
?,
?)
`,
		gameID.String(),
		platformInstanceID,
		metadataID.String(),
		contentID.String(),
		strings.ToLower(metadata.Title),
		now,
		now,
	); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	screenshotIDs, err := service.copyReviewAssets(
		ctx,
		transaction,
		itemID,
		gameID.String(),
		metadataID.String(),
		coverID,
		backgroundID,
		now,
	)
	if err != nil {
		return Approved{}, err
	}
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT role,
logical_name,
blob_id,
source_archive_blob_id,
source_archive_entry_ordinal,
sort_order
FROM import_item_source_files
WHERE import_item_id=?
ORDER BY sort_order,
role,
logical_name
`,
		itemID,
	)
	if err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var role, logicalName, blobID string
		var archiveID sql.NullString
		var archiveOrdinal sql.NullInt64
		var sortOrder int64
		if err := rows.Scan(&role, &logicalName, &blobID, &archiveID, &archiveOrdinal, &sortOrder); err != nil {
			return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_files(game_content_revision_id,
role,
logical_name,
blob_id,
source_archive_blob_id,
source_archive_entry_ordinal,
sort_order) VALUES(?,
?,
?,
?,
?,
?,
?)
`,
			contentID.String(),
			role,
			logicalName,
			blobID,
			nullable(archiveID),
			nullableInt(archiveOrdinal),
			sortOrder,
		); err != nil {
			return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	var emulatorGameID int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(emulator_game_id),
1000)+1
FROM game_variant_revisions
`).Scan(&emulatorGameID); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	validationDigest := sha256.Sum256([]byte(validationID))
	defaultDOSEntry := validationDOSEntry
	if draftDOSEntry.Valid {
		defaultDOSEntry = draftDOSEntry
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variants(id,
game_id,
core_id,
current_revision_id,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
NULL,
1,
?,
?)
`, variantID.String(), gameID.String(), coreID, now, now); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variant_revisions(id,
game_variant_id,
game_content_revision_id,
core_artifact_id,
dat_version_id,
validation_input_digest,
emulator_game_id,
status,
compatibility_code,
dependency_snapshot_json,
default_dos_entry,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
'READY',
'READY',
?,
?,
?)
`,
		variantRevisionID.String(),
		variantID.String(),
		contentID.String(),
		artifactID,
		nullable(datID),
		hex.EncodeToString(validationDigest[:]),
		emulatorGameID,
		dependencySnapshotJSON,
		nullable(defaultDOSEntry),
		now,
	); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,
role,
logical_name,
blob_id,
sort_order) SELECT ?,
role,
logical_name,
blob_id,
sort_order
FROM import_item_validation_files
WHERE import_item_core_validation_id=?
`, variantRevisionID.String(), validationID); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	var dependencySnapshot struct {
		Dependencies []struct {
			Kind            string   `json:"kind"`
			Machine         string   `json:"machine"`
			State           string   `json:"state"`
			RequiredEntries []string `json:"requiredEntries"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(dependencySnapshotJSON), &dependencySnapshot); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	for _, dependency := range dependencySnapshot.Dependencies {
		if !datID.Valid || (dependency.Kind != "PARENT" && dependency.Kind != "BIOS_OR_BASE") {
			return Approved{}, ErrInvalid
		}
		requiredEntries, _ := json.Marshal(dependency.RequiredEntries)
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_dependencies(game_variant_revision_id,
kind,
logical_archive,
dat_version_id,
source_machine_name,
required_entries_json,
state,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?)
`,
			variantRevisionID.String(),
			dependency.Kind,
			dependency.Machine+".zip",
			datID.String,
			dependency.Machine,
			string(requiredEntries),
			dependency.State,
			now,
		); err != nil {
			return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO dos_entries(game_content_revision_id,
normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe) SELECT ?,
normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM import_item_dos_entries
WHERE import_item_id=?
`, contentID.String(), itemID); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET current_revision_id=?,
updated_at_ms=?
WHERE id=?
`, variantRevisionID.String(), now, variantID.String()); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	beforeJSON, _ := json.Marshal(
		map[string]any{
			"schemaVersion":        1,
			"metadata":             json.RawMessage(metadataJSON),
			"selectedValidationId": validationID,
			"selectedCandidateId":  nullable(candidateID),
			"selectedAssets": map[string]any{
				"coverCandidateAssetId":       nullable(coverID),
				"backgroundCandidateAssetId":  nullable(backgroundID),
				"screenshotCandidateAssetIds": screenshotIDs,
			},
			"defaultDosEntry": nullable(draftDOSEntry),
		},
	)
	afterJSON, _ := json.Marshal(
		map[string]any{
			"schemaVersion":      1,
			"gameId":             gameID.String(),
			"metadataRevisionId": metadataID.String(),
			"contentRevisionId":  contentID.String(),
			"variantRevisionId":  variantRevisionID.String(),
		},
	)
	diffJSON, _ := json.Marshal(map[string]any{"schemaVersion": 1, "decision": "APPROVED"})
	configEvidenceJSON, _ := json.Marshal(
		map[string]any{
			"schemaVersion":  1,
			"configSnapshot": json.RawMessage(configSnapshotJSON),
			"validationId":   validationID,
		},
	)
	datEvidenceJSON, _ := json.Marshal(
		map[string]any{
			"schemaVersion":      1,
			"datVersionId":       nullable(datID),
			"dependencySnapshot": json.RawMessage(dependencySnapshotJSON),
		},
	)
	providerEvidenceJSON, _ := json.Marshal(
		map[string]any{
			"schemaVersion":               1,
			"selectedCandidateId":         nullable(candidateID),
			"coverCandidateAssetId":       nullable(coverID),
			"backgroundCandidateAssetId":  nullable(backgroundID),
			"screenshotCandidateAssetIds": screenshotIDs,
		},
	)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,
import_item_id,
event_type,
actor,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
reason,
created_at_ms) VALUES(?,
?,
'APPROVED',
'local',
?,
?,
?,
?,
?,
?,
?,
?)
`,
		eventID.String(),
		itemID,
		string(beforeJSON),
		string(afterJSON),
		string(diffJSON),
		string(configEvidenceJSON),
		string(datEvidenceJSON),
		string(providerEvidenceJSON),
		decisionReason,
		now,
	); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='PUBLISHED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
`, now, now, itemID); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET review_pending_item_count=review_pending_item_count-1,
published_item_count=published_item_count+1,
state=CASE WHEN review_pending_item_count=1
AND rejected_file_count=0 THEN 'COMPLETED' WHEN review_pending_item_count=1 THEN 'PARTIAL_FAILURE' ELSE state END,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN review_pending_item_count=1
AND rejected_file_count=0 THEN ? ELSE NULL END
WHERE id=?
`, now, now, importID); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	return Approved{GameID: gameID.String(), EventID: eventID.String(), Status: "PUBLISHED"}, nil
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (service *Service) copyReviewAssets(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID, metadataID string,
	coverID, backgroundID sql.NullString,
	now int64,
) ([]string, error) {
	copyAsset := func(assetID, kind string, ordinal int) error {
		var blobID, mediaType string
		var width, height int64
		err := transaction.QueryRowContext(ctx, `
SELECT a.blob_id,
a.width_px,
a.height_px,
a.media_type
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=?
AND a.status='READY'
AND r.import_item_id=?
AND r.state='COMPLETED'
`, assetID, itemID).Scan(&blobID, &width, &height, &mediaType)
		if err != nil {
			return ErrInvalid
		}
		assetUUID, _ := uuid.NewV7()
		_, err = transaction.ExecContext(
			ctx,
			`
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
			assetUUID.String(),
			gameID,
			metadataID,
			blobID,
			kind,
			ordinal,
			width,
			height,
			mediaType,
			now,
		)
		if err != nil {
			return fmt.Errorf("copy approved review asset: %w", err)
		}
		return nil
	}
	if coverID.Valid {
		if err := copyAsset(coverID.String, "COVER", 0); err != nil {
			return nil, err
		}
	}
	if backgroundID.Valid {
		if err := copyAsset(backgroundID.String, "BACKGROUND", 0); err != nil {
			return nil, err
		}
	}
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT s.candidate_asset_id
FROM review_draft_screenshot_assets s
JOIN review_drafts d ON d.id=s.review_draft_id
WHERE d.import_item_id=?
ORDER BY s.ordinal
`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	screenshotIDs := make([]string, 0)
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, fmt.Errorf("libraryimport/service: %w", err)
		}
		screenshotIDs = append(screenshotIDs, assetID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	for ordinal, assetID := range screenshotIDs {
		if err := copyAsset(assetID, "SCREENSHOT", ordinal); err != nil {
			return nil, err
		}
	}
	return screenshotIDs, nil
}

func nullableInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}
