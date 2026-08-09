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

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentmanifest"
	"retrom/internal/contentprofile"
	"retrom/internal/corevalidation"
	"retrom/internal/importing"
	"retrom/internal/metadatascrape"
)

var (
	ErrInvalid                        = errors.New("IMPORT_INVALID")
	ErrReimportRequiredPlatformChange = errors.New("REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE")
	errMetadataScraperNotConfigured   = errors.New("metadata scraper is not configured")
)

type CreateRequest struct {
	UploadID                 string `json:"uploadId"`
	TargetPlatformInstanceID string `json:"targetPlatformInstanceId"`
	MetadataProvider         string `json:"metadataProvider"`
}

type ReconfigureRequest struct {
	TargetPlatformInstanceID string `json:"targetPlatformInstanceId"`
	MetadataProvider         string `json:"metadataProvider"`
}

type Created struct {
	ImportJobID string `json:"importJobId"`
	JobID       string `json:"jobId"`
	State       string `json:"state"`
	ItemCount   int    `json:"itemCount"`
}

type initialImportProgress struct {
	state              string
	itemState          string
	runningItems       int
	reviewPendingItems int
	completed          bool
}

func newInitialImportProgress(metadataProvider string, itemCount, rejectedFileCount int) initialImportProgress {
	if itemCount == 0 {
		if rejectedFileCount > 0 {
			return initialImportProgress{state: "PARTIAL_FAILURE", itemState: "REVIEW_PENDING"}
		}
		return initialImportProgress{state: "COMPLETED", itemState: "REVIEW_PENDING", completed: true}
	}
	if metadataProvider == "HASHEOUS" {
		return initialImportProgress{
			state: "RUNNING", itemState: "SCRAPING", runningItems: itemCount,
		}
	}
	state := "REVIEW_PENDING"
	if rejectedFileCount > 0 {
		state = "PARTIAL_FAILURE"
	}
	return initialImportProgress{
		state: state, itemState: "REVIEW_PENDING", reviewPendingItems: itemCount,
	}
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

type reconfigurationInput struct {
	sourceImportJobID string
	sourceVersion     int64
	sourceFileIDs     []string
}

type reusableUploadFile struct {
	id, path, blobID string
	size             int64
}

func knownSidecar(path string) bool {
	base := filepath.Base(path)
	return base == ".DS_Store" || base == "Thumbs.db" || strings.HasPrefix(base, "._")
}

func archiveReason(err error) string {
	switch {
	case errors.Is(err, importing.ErrArchiveLimitExceeded):
		return "ARCHIVE_LIMIT_EXCEEDED"
	case errors.Is(err, importing.ErrArchiveEncrypted):
		return "ARCHIVE_ENCRYPTED_UNSUPPORTED"
	case errors.Is(err, importing.ErrArchiveVolumeUnsupported):
		return "ARCHIVE_VOLUME_UNSUPPORTED"
	case errors.Is(err, importing.ErrArchiveResourceLimit):
		return "ARCHIVE_RESOURCE_LIMIT"
	case errors.Is(err, importing.ErrArchiveSandboxUnavailable):
		return "ARCHIVE_SANDBOX_UNAVAILABLE"
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
	var metadata blobstore.Metadata
	var putErr, closeErr error
	switch expected.ArchiveFormat {
	case "ZIP":
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
		metadata, putErr = service.blobs.Put(io.LimitReader(entry, expected.Size+1))
		closeErr = entry.Close()
	case "SEVEN_Z":
		reader, writer := io.Pipe()
		done := make(chan error, 1)
		go func() {
			extractErr := importing.ExtractSevenZip(ctx, archivePath, expected, writer)
			_ = writer.CloseWithError(extractErr)
			done <- extractErr
		}()
		metadata, putErr = service.blobs.Put(io.LimitReader(reader, expected.Size+1))
		closeErr = errors.Join(reader.Close(), <-done)
	default:
		return blobstore.Metadata{}, importing.ErrArchiveUnsafe
	}
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

func rankDOSEntries(entries []preparedDOSEntry) {
	sort.SliceStable(entries, func(left, right int) bool {
		leftCategory, leftExtension, leftDepth, leftPath := dosEntryPriority(entries[left])
		rightCategory, rightExtension, rightDepth, rightPath := dosEntryPriority(entries[right])
		if leftCategory != rightCategory {
			return leftCategory < rightCategory
		}
		if leftExtension != rightExtension {
			return leftExtension < rightExtension
		}
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return leftPath < rightPath
	})
	for index := range entries {
		entries[index].rank = index
	}
}

func dosEntryPriority(entry preparedDOSEntry) (int, int, int, string) {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(entry.path)), strings.ToLower(filepath.Ext(entry.path)))
	name := strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, base)
	category := 1
	preferred := map[string]struct{}{
		"game": {}, "go": {}, "launch": {}, "play": {}, "run": {}, "start": {},
	}
	helpers := map[string]struct{}{
		"arj": {}, "backup": {}, "config": {}, "configure": {}, "dos32a": {}, "dos4gw": {},
		"end": {}, "help": {}, "install": {}, "installer": {}, "pkunzip": {}, "readme": {},
		"register": {}, "restore": {}, "setup": {}, "setsound": {}, "uninstall": {}, "update": {},
	}
	if _, matched := preferred[name]; matched {
		category = 0
	} else if _, matched := helpers[name]; matched {
		category = 2
	}
	extension := map[string]int{"EXE": 0, "COM": 1, "BAT": 2}[entry.kind]
	return category, extension, strings.Count(entry.path, "/"), strings.ToLower(entry.path)
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
		entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DOSArchiveLimits())
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
		rankDOSEntries(programs)
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
	rankDOSEntries(programs)
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
		requiredNames[importing.ASCIICaseFold(requirement.name)] = struct{}{}
	}
	for path := range entries {
		if !strings.Contains(path, "/") {
			continue
		}
		if _, required := requiredNames[importing.ASCIICaseFold(filepath.Base(path))]; required {
			return true
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
	dependencyCandidates := make(map[string]struct{})
	if datID.Valid {
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
					dependencyCandidates[machine] = struct{}{}
				}
			}
		}
	}
	referenced := make(map[string]struct{})
	groups := make([]preparedGroup, 0)
	for index := range prepared {
		primary := &prepared[index]
		if primary.reason != "" || primary.classification != "NORMAL" {
			continue
		}
		if _, dependencyOnly := dependencyCandidates[primary.machine]; dependencyOnly {
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
		closureByMachine := make(map[string]arcadeClosureNode, len(closure))
		for _, node := range closure {
			closureByMachine[node.Machine] = node
		}
		addDependencies := func(names []string, kind, role string) {
			for _, name := range names {
				node := closureByMachine[name]
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
							"kind": kind, "machine": name, "requiredBy": node.RequiredBy, "depth": node.Depth,
							"expectedLogicalName": name + ".zip", "state": "SATISFIED_BY_CONTENT",
							"requiredEntryCount": len(requiredEntries), "requiredEntries": requiredEntries,
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
							"kind": kind, "machine": name, "requiredBy": node.RequiredBy, "depth": node.Depth,
							"expectedLogicalName": name + ".zip", "state": "MISSING",
							"requiredEntryCount": len(requiredEntries), "requiredEntries": requiredEntries,
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
							"kind": kind, "machine": name, "requiredBy": node.RequiredBy, "depth": node.Depth,
							"expectedLogicalName": name + ".zip", "state": "MISMATCH",
							"requiredEntryCount": len(requiredEntries), "requiredEntries": requiredEntries,
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
						"kind": kind, "machine": name, "requiredBy": node.RequiredBy, "depth": node.Depth,
						"expectedLogicalName": name + ".zip", "state": dependencyState,
						"requiredEntryCount": len(requiredEntries), "requiredEntries": requiredEntries,
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
		snapshot, _ := json.Marshal(
			map[string]any{
				"schemaVersion":     2,
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
	profile, profileExists := contentprofile.ByPlatform(platformID)
	if !profileExists {
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
		if contentprofile.AcceptsRaw(platformID, file.path) {
			dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
			groups = append(
				groups,
				preparedGroup{
					sources: []preparedSource{{file: file, role: "CONTENT", logicalName: filepath.Base(file.path)}},
				},
			)
			continue
		}
		archiveFormat := contentprofile.ArchiveZIP
		switch extension {
		case ".zip":
		case ".7z":
			archiveFormat = contentprofile.ArchiveSevenZip
		default:
			if strings.HasSuffix(strings.ToLower(file.path), ".7z.001") {
				dispositions = append(dispositions, preparedDisposition{
					file: file, disposition: "REJECTED", reason: "ARCHIVE_VOLUME_UNSUPPORTED",
				})
				continue
			}
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "UNSUPPORTED_CONTENT_FORMAT"},
			)
			continue
		}
		if service.blobs == nil || profile.ArchivePolicy != contentprofile.ArchiveSinglePrimary ||
			!contentprofile.AcceptsArchive(platformID, archiveFormat) {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "UNSUPPORTED_CONTENT_FORMAT"},
			)
			continue
		}
		var entries []importing.ArchiveEntry
		var err error
		if archiveFormat == contentprofile.ArchiveZIP {
			entries, err = importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DefaultArchiveLimits())
		} else {
			entries, err = importing.ScanSevenZip(ctx, service.blobs.Path(file.sha256), importing.DefaultArchiveLimits())
		}
		if err != nil {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: archiveReason(err)},
			)
			continue
		}
		candidate, selectErr := contentprofile.SelectArchivePrimary(platformID, entries)
		if errors.Is(selectErr, contentprofile.ErrNoSupportedContent) {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "NO_SUPPORTED_CONTENT"},
			)
			continue
		}
		if errors.Is(selectErr, contentprofile.ErrAmbiguousPrimaryContent) {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: "AMBIGUOUS_PRIMARY_CONTENT"},
			)
			continue
		}
		selected, err := service.materializeArchiveEntry(ctx, service.blobs.Path(file.sha256), candidate)
		if err != nil {
			dispositions = append(
				dispositions,
				preparedDisposition{file: file, disposition: "REJECTED", reason: archiveReason(err)},
			)
			continue
		}
		ordinal := candidate.Ordinal
		dispositions = append(dispositions, preparedDisposition{file: file, disposition: "SOURCE"})
		groups = append(
			groups,
			preparedGroup{
				sources: []preparedSource{
					{
						file:           file,
						role:           "CONTENT",
						logicalName:    filepath.Base(candidate.NormalizedPath),
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

func reviewActor(ctx context.Context) authn.Actor {
	return authn.ActorFromContext(ctx, "release-setup")
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

// Reconfigure reuses the unresolved rejected files from an existing import. The
// cloned UploadSession owns new logical UploadFiles but points at the same CAS
// blobs, so the browser never has to upload the bytes again.
func (service *Service) Reconfigure(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
	request ReconfigureRequest,
) (Created, error) {
	if sourceImportJobID == "" || expectedVersion < 1 {
		return Created{}, ErrInvalid
	}
	sourceType, files, err := service.reconfigurationSource(ctx, sourceImportJobID, expectedVersion)
	if err != nil {
		return Created{}, err
	}
	if len(files) == 0 {
		return Created{}, ErrInvalid
	}
	uploadID, err := service.cloneUploadSession(ctx, sourceImportJobID, expectedVersion, sourceType, files)
	if err != nil {
		return Created{}, err
	}
	sourceFileIDs := make([]string, 0, len(files))
	for _, file := range files {
		sourceFileIDs = append(sourceFileIDs, file.id)
	}
	created, err := service.create(
		ctx,
		CreateRequest{
			UploadID:                 uploadID,
			TargetPlatformInstanceID: request.TargetPlatformInstanceID,
			MetadataProvider:         request.MetadataProvider,
		},
		&reconfigurationInput{
			sourceImportJobID: sourceImportJobID,
			sourceVersion:     expectedVersion,
			sourceFileIDs:     sourceFileIDs,
		},
	)
	if err != nil {
		service.removeUnusedClonedUpload(ctx, uploadID)
		return Created{}, err
	}
	return created, nil
}

func (service *Service) reconfigurationSource(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
) (string, []reusableUploadFile, error) {
	var sourceType, state string
	var currentVersion int64
	if err := service.database.QueryRowContext(ctx, `
SELECT u.source_type,
i.state,
i.version
FROM import_jobs i
JOIN upload_sessions u ON u.id=i.upload_session_id
WHERE i.id=?
`, sourceImportJobID).Scan(&sourceType, &state, &currentVersion); err != nil ||
		state != "PARTIAL_FAILURE" || currentVersion != expectedVersion {
		return "", nil, ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT u.id,
u.relative_path,
u.declared_size_bytes,
u.final_blob_id
FROM import_job_files f
JOIN upload_files u ON u.id=f.upload_file_id
LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id
AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=?
AND f.disposition='REJECTED'
AND resolution.upload_file_id IS NULL
ORDER BY u.relative_path,
u.id
`, sourceImportJobID)
	if err != nil {
		return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]reusableUploadFile, 0)
	for rows.Next() {
		var file reusableUploadFile
		if err := rows.Scan(&file.id, &file.path, &file.size, &file.blobID); err != nil {
			return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	if err := rows.Close(); err != nil {
		return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	return sourceType, files, nil
}

func (service *Service) cloneUploadSession(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
	sourceType string,
	files []reusableUploadFile,
) (string, error) {
	uploadID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	digest := reconfigurationManifestDigest(sourceImportJobID, expectedVersion, files)
	now := service.now().UnixMilli()
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.size
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentState string
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version
FROM import_jobs
WHERE id=?
`, sourceImportJobID).Scan(&currentState, &currentVersion); err != nil ||
		currentState != "PARTIAL_FAILURE" || currentVersion != expectedVersion {
		return "", ErrInvalid
	}
	if err := insertClonedUpload(
		ctx, transaction, uploadID.String(), sourceType, files, digest, now, totalBytes,
	); err != nil {
		return "", err
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	return uploadID.String(), nil
}

func reconfigurationManifestDigest(sourceImportJobID string, sourceVersion int64, files []reusableUploadFile) string {
	manifestFiles := make([]map[string]any, 0, len(files))
	for _, file := range files {
		manifestFiles = append(manifestFiles, map[string]any{
			"sourceUploadFileId": file.id,
			"relativePath":       file.path,
			"sizeBytes":          file.size,
			"blobId":             file.blobID,
		})
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion":     1,
		"sourceImportJobId": sourceImportJobID,
		"sourceVersion":     sourceVersion,
		"files":             manifestFiles,
	})
	digest := sha256.Sum256(manifest)
	return hex.EncodeToString(digest[:])
}

func insertClonedUpload(
	ctx context.Context,
	transaction *sql.Tx,
	uploadID, sourceType string,
	files []reusableUploadFile,
	manifestDigest string,
	now, totalBytes int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_sessions(id,
state,
source_type,
total_files,
total_bytes,
manifest_digest,
version,
expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'COMPLETE',
?,
?,
?,
?,
1,
?,
?,
?)
`, uploadID, sourceType, len(files), totalBytes, manifestDigest,
		now+int64(24*time.Hour/time.Millisecond), now, now); err != nil {
		return fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	for _, file := range files {
		fileID, idErr := uuid.NewV7()
		if idErr != nil {
			return fmt.Errorf("libraryimport/reconfigure: %w", idErr)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_files(id,
upload_session_id,
relative_path,
declared_size_bytes,
received_size_bytes,
final_blob_id,
state,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
'COMPLETE',
?,
?)
`, fileID.String(), uploadID, file.path, file.size, file.size, file.blobID, now, now); err != nil {
			return fmt.Errorf("libraryimport/reconfigure: %w", err)
		}
	}
	return nil
}

func (service *Service) removeUnusedClonedUpload(ctx context.Context, uploadID string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	var consumptionCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM upload_consumptions
WHERE upload_session_id=?
`, uploadID).Scan(&consumptionCount); err != nil || consumptionCount != 0 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM upload_files WHERE upload_session_id=?`, uploadID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id=?`, uploadID); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) Create(ctx context.Context, request CreateRequest) (Created, error) {
	return service.create(ctx, request, nil)
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) create(
	ctx context.Context,
	request CreateRequest,
	reconfiguration *reconfigurationInput,
) (Created, error) {
	if request.MetadataProvider != "NONE" && request.MetadataProvider != "HASHEOUS" {
		return Created{}, ErrInvalid
	}
	if request.MetadataProvider == "HASHEOUS" && service.scraper == nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", errMetadataScraperNotConfigured)
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
	biosCatalog, err := corevalidation.Catalog(ctx, transaction, artifactID)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := prepareStaticBIOSDependencies(ctx, transaction, artifactID, platformID, groups); err != nil {
		return Created{}, err
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
		"biosRequirements":              biosCatalog,
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
	progress := newInitialImportProgress(request.MetadataProvider, len(groups), rejected)
	resultState := progress.state
	alreadyImportedItems := 0
	sourceFileItemCounts := make(map[string]int)
	duplicateFileItemCounts := make(map[string]int)
	completedAt := any(nil)
	if progress.completed {
		completedAt = now
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
running_item_count,
review_pending_item_count,
ignored_file_count,
rejected_file_count,
version,
created_at_ms,
updated_at_ms,
completed_at_ms) VALUES(?,
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
?,
1,
?,
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
		progress.state,
		len(groups),
		progress.runningItems,
		progress.reviewPendingItems,
		ignored,
		rejected,
		now,
		now,
		completedAt,
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
archive_format,
compression_profile,
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
?,
?)
`,
				archive.blobID,
				entry.Ordinal,
				entry.OriginalPath,
				entry.NormalizedPath,
				entry.ASCIICasefoldPath,
				entry.ArchiveFormat,
				entry.CompressionProfile,
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
		sourceSnapshotID, _ := uuid.NewV7()
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
 ?,
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
			progress.itemState,
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
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(id,
import_item_id,
revision_no,
source_manifest_json,
source_manifest_digest,
created_by,
created_at_ms) VALUES(?,?,1,?,?,'IDENTIFICATION',?)
`, sourceSnapshotID.String(), itemID.String(), string(manifestJSON), manifestDigestHex, now); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,
role,
logical_name,
upload_file_id,
blob_id,
source_archive_blob_id,
source_archive_entry_ordinal,
sort_order,
created_at_ms)
SELECT ?,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,
source_archive_entry_ordinal,sort_order,created_at_ms
FROM import_item_source_files
WHERE import_item_id=?
`, sourceSnapshotID.String(), itemID.String()); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		groupUploadFileIDs := make(map[string]struct{})
		for _, source := range group.sources {
			groupUploadFileIDs[source.file.id] = struct{}{}
		}
		for uploadFileID := range groupUploadFileIDs {
			sourceFileItemCounts[uploadFileID]++
		}
		contentIdentityDigest, err := importItemContentIdentity(ctx, transaction, itemID.String())
		if err != nil {
			return Created{}, err
		}
		duplicateGames, err := findDuplicateGames(ctx, transaction, itemID.String(), platformID)
		if err != nil {
			return Created{}, err
		}
		if len(duplicateGames) > 0 {
			if err := claimContentIdentity(ctx, transaction, platformID, contentIdentityDigest, now); err != nil {
				return Created{}, err
			}
			for _, game := range duplicateGames {
				if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_duplicate_matches(import_item_id,
existing_game_id,
existing_game_content_revision_id,
content_identity_digest,
detected_stage,
created_at_ms) VALUES(?,
?,
?,
?,
'IDENTIFICATION',
?)
`, itemID.String(), game.GameID, game.CurrentContentRevisionID, contentIdentityDigest, now); err != nil {
					return Created{}, fmt.Errorf("libraryimport/service: %w", err)
				}
			}
			if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='DISCARDED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
`, now, now, itemID.String()); err != nil {
				return Created{}, fmt.Errorf("libraryimport/service: %w", err)
			}
			alreadyImportedItems++
			for uploadFileID := range groupUploadFileIDs {
				duplicateFileItemCounts[uploadFileID]++
			}
			continue
		}
		validationInput := append([]byte("arcade-source-validator-v3\x00"), manifestDigest[:]...)
		validationInput = append(validationInput, configDigest[:]...)
		validationInput = append(validationInput, []byte(sourceSnapshotID.String())...)
		inputDigest := sha256.Sum256(validationInput)
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
source_snapshot_id,
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
			sourceSnapshotID.String(),
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
effective_source_snapshot_id,
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
?,
1,
?,
?)
`,
			draftID.String(),
			itemID.String(),
			request.TargetPlatformInstanceID,
			selectedValidation,
			sourceSnapshotID.String(),
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
	if alreadyImportedItems > 0 {
		alreadyImportedFileCount := 0
		for uploadFileID, duplicateItemCount := range duplicateFileItemCounts {
			if duplicateItemCount == sourceFileItemCounts[uploadFileID] {
				alreadyImportedFileCount++
			}
		}
		remainingItems := len(groups) - alreadyImportedItems
		runningItems := 0
		reviewPendingItems := 0
		completedAt = nil
		switch {
		case remainingItems == 0 && rejected > 0:
			resultState = "PARTIAL_FAILURE"
		case remainingItems == 0:
			resultState = "COMPLETED"
			completedAt = now
		case request.MetadataProvider == "HASHEOUS":
			resultState = "RUNNING"
			runningItems = remainingItems
		case rejected > 0:
			resultState = "PARTIAL_FAILURE"
			reviewPendingItems = remainingItems
		default:
			resultState = "REVIEW_PENDING"
			reviewPendingItems = remainingItems
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET state=?,
running_item_count=?,
review_pending_item_count=?,
discarded_item_count=?,
already_imported_item_count=?,
already_imported_file_count=?,
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
`,
			resultState,
			runningItems,
			reviewPendingItems,
			alreadyImportedItems,
			alreadyImportedItems,
			alreadyImportedFileCount,
			now,
			completedAt,
			importID.String(),
		); err != nil {
			return Created{}, fmt.Errorf("libraryimport/service: %w", err)
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
	if reconfiguration != nil {
		if err := recordImportReconfiguration(
			ctx,
			transaction,
			*reconfiguration,
			importID.String(),
			now,
		); err != nil {
			return Created{}, err
		}
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
	return Created{
		ImportJobID: importID.String(), JobID: jobID.String(), State: resultState, ItemCount: len(groups),
	}, nil
}

//nolint:funlen // Resolution rows and aggregate repair must commit as one auditable operation.
func recordImportReconfiguration(
	ctx context.Context,
	transaction *sql.Tx,
	input reconfigurationInput,
	replacementImportJobID string,
	now int64,
) error {
	actor := reviewActor(ctx)
	for _, uploadFileID := range input.sourceFileIDs {
		result, err := transaction.ExecContext(ctx, `
INSERT INTO import_job_file_resolutions(import_job_id,
upload_file_id,
action,
replacement_import_job_id,
actor_kind,
actor_user_id,
actor_label,
created_at_ms)
SELECT f.import_job_id,
f.upload_file_id,
'RECONFIGURED',
?,
?,
?,
?,
?
FROM import_job_files f
LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id
AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=?
AND f.upload_file_id=?
AND f.disposition='REJECTED'
AND resolution.upload_file_id IS NULL
`, replacementImportJobID, actor.Kind, actor.UserID, actor.Label, now, input.sourceImportJobID, uploadFileID)
		if err != nil {
			return fmt.Errorf("libraryimport/reconfigure: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil || inserted != 1 {
			return ErrInvalid
		}
	}
	resolved := int64(len(input.sourceFileIDs))
	result, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET resolved_rejected_file_count=resolved_rejected_file_count+?,
state=CASE
  WHEN queued_item_count>0 OR running_item_count>0 THEN 'RUNNING'
  WHEN failed_item_count>0 OR rejected_file_count>resolved_rejected_file_count+? THEN 'PARTIAL_FAILURE'
  WHEN review_pending_item_count>0 THEN 'REVIEW_PENDING'
  ELSE 'COMPLETED'
END,
completed_at_ms=CASE
  WHEN queued_item_count=0
   AND running_item_count=0
   AND failed_item_count=0
   AND rejected_file_count=resolved_rejected_file_count+?
   AND review_pending_item_count=0 THEN ?
  ELSE NULL
END,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
AND state='PARTIAL_FAILURE'
AND resolved_rejected_file_count+?<=rejected_file_count
`, resolved, resolved, resolved, now, now, input.sourceImportJobID, input.sourceVersion, resolved)
	if err != nil {
		return fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET reconfigured_from_import_job_id=?
WHERE id=?
`, input.sourceImportJobID, replacementImportJobID); err != nil {
		return fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	return nil
}

func prepareStaticBIOSDependencies(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, platformID string,
	groups []preparedGroup,
) error {
	if platformID == "arcade" {
		return nil
	}
	for index := range groups {
		logicalName := ""
		for _, source := range groups[index].sources {
			if source.role == "CONTENT" {
				logicalName = source.logicalName
				break
			}
		}
		if logicalName == "" && platformID != "dos" {
			return ErrInvalid
		}
		snapshot, status, code, err := corevalidation.ResolveBIOS(ctx, transaction, artifactID, logicalName)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		snapshotJSON, err := snapshot.JSON()
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		groups[index].validationStatus = status
		groups[index].compatibilityCode = code
		groups[index].dependencySnapshot = string(snapshotJSON)
		for _, dependency := range snapshot.BIOS {
			if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
				groups[index].validationFiles = append(groups[index].validationFiles, preparedValidationFile{
					role: "BIOS_BUNDLE", logicalName: dependency.LogicalName,
					blobID: *dependency.BlobID, sortOrder: len(groups[index].validationFiles),
				})
			}
		}
	}
	return nil
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

type ApprovalDecision struct {
	Reason              *string
	DuplicatePolicy     string
	AcknowledgedGameIDs []string
}

func (service *Service) Approve(ctx context.Context, itemID string, expectedVersion int64) (Approved, error) {
	return service.ApproveWithDecision(ctx, itemID, expectedVersion, ApprovalDecision{})
}

func (service *Service) ApproveWithReason(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	reason *string,
) (Approved, error) {
	return service.ApproveWithDecision(ctx, itemID, expectedVersion, ApprovalDecision{Reason: reason})
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) ApproveWithDecision(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	decision ApprovalDecision,
) (Approved, error) {
	var decisionReason any
	if decision.Reason != nil {
		trimmed := strings.TrimSpace(*decision.Reason)
		if trimmed == "" || !validField(trimmed, 500, true) {
			return Approved{}, ErrInvalid
		}
		decisionReason = trimmed
	}
	if decision.DuplicatePolicy != "" && decision.DuplicatePolicy != "ALLOW_NEW" {
		return Approved{}, ErrInvalid
	}
	if decision.DuplicatePolicy == "" && len(decision.AcknowledgedGameIDs) != 0 {
		return Approved{}, ErrInvalid
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
	var state, importID, configSnapshotJSON, platformID, platformInstanceID, validationID string
	var metadataJSON, sourceSnapshotID, sourceManifestJSON, sourceManifestDigest, dependencySnapshotJSON string
	var coreID, artifactID string
	var draftVersion int64
	var datID, validationDOSEntry, draftDOSEntry, candidateID, coverID, uploadedCoverID, backgroundID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT i.state,
i.import_job_id,
j.config_snapshot_json,
p.platform_id,
d.target_platform_instance_id,
d.selected_validation_id,
d.metadata_json,
source_snapshot.id,
source_snapshot.source_manifest_json,
source_snapshot.source_manifest_digest,
v.core_id,
v.core_artifact_id,
v.dat_version_id,
v.default_dos_entry,
d.default_dos_entry,
v.dependency_snapshot_json,
d.version,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
JOIN import_item_core_validations v ON v.id=d.selected_validation_id
AND v.source_snapshot_id=d.effective_source_snapshot_id
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
			&platformID,
			&platformInstanceID,
			&validationID,
			&metadataJSON,
			&sourceSnapshotID,
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
			&uploadedCoverID,
			&backgroundID,
		)
	if err != nil || state != "REVIEW_PENDING" || draftVersion != expectedVersion {
		return Approved{}, ErrInvalid
	}
	validationSnapshot, snapshotErr := corevalidation.ParseSnapshot(dependencySnapshotJSON)
	if snapshotErr == nil {
		var contentLogicalName string
		if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(CASE WHEN role='CONTENT' THEN logical_name END),'')
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=?
`, sourceSnapshotID).Scan(&contentLogicalName); err != nil {
			return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
		}
		currentSnapshot, validationStatus, _, resolveErr := corevalidation.ResolveBIOS(
			ctx,
			transaction,
			artifactID,
			contentLogicalName,
		)
		if resolveErr != nil || validationStatus != "READY" {
			return Approved{}, ErrInvalid
		}
		currentJSON, marshalErr := currentSnapshot.JSON()
		if marshalErr != nil || string(currentJSON) != dependencySnapshotJSON {
			return Approved{}, ErrInvalid
		}
	}
	var metadata struct {
		Title, Description, Developer, Publisher, Genre string
		Players, ReleaseYear                            *int
	}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil || strings.TrimSpace(metadata.Title) == "" {
		return Approved{}, ErrInvalid
	}
	now := service.now().UnixMilli()
	contentIdentityDigest, err := importItemContentIdentity(ctx, transaction, itemID)
	if err != nil {
		return Approved{}, err
	}
	if err := claimContentIdentity(ctx, transaction, platformID, contentIdentityDigest, now); err != nil {
		return Approved{}, err
	}
	duplicateGames, err := findDuplicateGames(ctx, transaction, itemID, platformID)
	if err != nil {
		return Approved{}, err
	}
	if len(duplicateGames) > 0 &&
		(decision.DuplicatePolicy != "ALLOW_NEW" ||
			!sameDuplicateIDs(duplicateGames, decision.AcknowledgedGameIDs)) {
		return Approved{}, &DuplicateConflict{
			ContentIdentityDigest: contentIdentityDigest,
			Games:                 duplicateGames,
		}
	}
	if len(duplicateGames) == 0 && decision.DuplicatePolicy != "" {
		return Approved{}, ErrInvalid
	}
	gameID, _ := uuid.NewV7()
	metadataID, _ := uuid.NewV7()
	contentID, _ := uuid.NewV7()
	variantID, _ := uuid.NewV7()
	variantRevisionID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
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
		uploadedCoverID,
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
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=?
ORDER BY sort_order,
role,
logical_name
`,
		sourceSnapshotID,
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
	validationInputDigest := ""
	if snapshotErr == nil {
		validationInputDigest, err = corevalidation.ValidationInputDigest(
			artifactID,
			contentID.String(),
			datID,
			validationSnapshot,
		)
		if err != nil {
			return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
		}
	} else {
		validationDigest := sha256.Sum256([]byte(validationID))
		validationInputDigest = hex.EncodeToString(validationDigest[:])
	}
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
		validationInputDigest,
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
			"schemaVersion":             1,
			"effectiveSourceSnapshotId": sourceSnapshotID,
			"metadata":                  json.RawMessage(metadataJSON),
			"selectedValidationId":      validationID,
			"selectedCandidateId":       nullable(candidateID),
			"selectedAssets": map[string]any{
				"coverCandidateAssetId":       nullable(coverID),
				"coverUploadedAssetId":        nullable(uploadedCoverID),
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
	diff := map[string]any{
		"schemaVersion":         1,
		"decision":              "APPROVED",
		"contentIdentityDigest": contentIdentityDigest,
	}
	if decision.DuplicatePolicy == "ALLOW_NEW" {
		diff["duplicatePolicy"] = decision.DuplicatePolicy
		diff["acknowledgedGameIds"] = duplicateIDs(duplicateGames)
	}
	diffJSON, _ := json.Marshal(diff)
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
			"coverUploadedAssetId":        nullable(uploadedCoverID),
			"backgroundCandidateAssetId":  nullable(backgroundID),
			"screenshotCandidateAssetIds": screenshotIDs,
		},
	)
	actor := reviewActor(ctx)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,
import_item_id,
event_type,
actor_kind,
actor_user_id,
actor_label,
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
		eventID.String(),
		itemID,
		actor.Kind,
		actor.UserID,
		actor.Label,
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
AND rejected_file_count=resolved_rejected_file_count THEN 'COMPLETED'
WHEN review_pending_item_count=1 THEN 'PARTIAL_FAILURE'
ELSE state END,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN review_pending_item_count=1
AND rejected_file_count=resolved_rejected_file_count THEN ? ELSE NULL END
WHERE id=?
`, now, now, importID); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Approved{}, fmt.Errorf("libraryimport/service: %w", err)
	}
	return Approved{GameID: gameID.String(), EventID: eventID.String(), Status: "PUBLISHED"}, nil
}

func copyCandidateReviewAsset(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID, metadataID, assetID, kind string,
	ordinal int,
	now int64,
) error {
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
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(
id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?)
`, assetUUID.String(), gameID, metadataID, blobID, kind, ordinal, width, height, mediaType, now); err != nil {
		return fmt.Errorf("copy approved review asset: %w", err)
	}
	return nil
}

func copyUploadedReviewCover(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID, metadataID, assetID string,
	now int64,
) error {
	var blobID, mediaType string
	var width, height int64
	if err := transaction.QueryRowContext(ctx, `
SELECT blob_id,width_px,height_px,media_type
FROM review_uploaded_assets
WHERE id=? AND import_item_id=? AND kind='COVER'
`, assetID, itemID).Scan(&blobID, &width, &height, &mediaType); err != nil {
		return ErrInvalid
	}
	assetUUID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(
id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?, 'COVER',0,?,?,?,?)
`, assetUUID.String(), gameID, metadataID, blobID, width, height, mediaType, now); err != nil {
		return fmt.Errorf("copy uploaded review cover: %w", err)
	}
	return nil
}

func (service *Service) copyReviewAssets(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID, metadataID string,
	coverID, uploadedCoverID, backgroundID sql.NullString,
	now int64,
) ([]string, error) {
	if coverID.Valid {
		if err := copyCandidateReviewAsset(
			ctx, transaction, itemID, gameID, metadataID, coverID.String, "COVER", 0, now,
		); err != nil {
			return nil, err
		}
	}
	if uploadedCoverID.Valid {
		if err := copyUploadedReviewCover(
			ctx, transaction, itemID, gameID, metadataID, uploadedCoverID.String, now,
		); err != nil {
			return nil, err
		}
	}
	if backgroundID.Valid {
		if err := copyCandidateReviewAsset(
			ctx, transaction, itemID, gameID, metadataID, backgroundID.String, "BACKGROUND", 0, now,
		); err != nil {
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
		if err := copyCandidateReviewAsset(
			ctx, transaction, itemID, gameID, metadataID, assetID, "SCREENSHOT", ordinal, now,
		); err != nil {
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
