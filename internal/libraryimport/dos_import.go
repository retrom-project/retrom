package libraryimport

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/importing"
)

var (
	ErrInvalid                        = errors.New("IMPORT_INVALID")
	ErrVersionConflict                = errors.New("VERSION_CONFLICT")
	ErrReimportRequiredPlatformChange = errors.New("REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE")
	ErrMultiDiscModeUnavailable       = errors.New("MULTI_DISC_MODE_UNAVAILABLE")
	ErrMultiDiscPlaylistMissing       = errors.New("MULTI_DISC_PLAYLIST_MISSING")
	errMetadataScraperNotConfigured   = errors.New("metadata scraper is not configured")
)

const reviewScreenshotOverrideCode = "REVIEW_SCREENSHOT_OVERRIDE"

type CreateRequest struct {
	UploadID                 string   `json:"uploadId"`
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	MetadataProvider         string   `json:"metadataProvider"`
	ContentMode              string   `json:"contentMode,omitempty"`
	TagIDs                   []string `json:"tagIds"`
}

type ReconfigureRequest struct {
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	MetadataProvider         string   `json:"metadataProvider"`
	TagIDs                   []string `json:"tagIds"`
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
	size                     int64
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
	sortOrder      *int
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
	contentKind        string
	groupKey           string
	multiEntries       []preparedMultiDiscEntry
	multiDependency    *corevalidation.MultiDiscSnapshot
	canonicalPlaylist  *blobstore.Metadata
}

type preparedMultiDiscEntry struct {
	ordinal                                             int
	state                                               string
	sourceReference, normalizedReference, canonicalName string
	uploadFileID, blobID, sourceLogicalName             string
}

type preparedValidationFile struct {
	role, logicalName, blobID string
	sortOrder                 int
}

type preparedDOSEntry struct {
	path, kind             string
	rank                   int
	safe                   bool
	batchContents          []byte
	inferredTerminalTarget bool
}

const maxDOSBatchInspectionBytes = 64 << 10

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
	inferDOSBatchTerminalTargets(entries)
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
	switch {
	case entry.inferredTerminalTarget:
		category = -1
	case preferredDOSProgramName(name):
		category = 0
	case helperDOSProgramName(name):
		category = 2
	}
	extension := map[string]int{"EXE": 0, "COM": 1, "BAT": 2}[entry.kind]
	return category, extension, strings.Count(entry.path, "/"), strings.ToLower(entry.path)
}

func preferredDOSProgramName(name string) bool {
	switch name {
	case "game", "go", "launch", "play", "run", "start":
		return true
	default:
		return false
	}
}

func helperDOSProgramName(name string) bool {
	switch name {
	case "arj", "backup", "config", "configure", "dos32a", "dos4gw", "end", "help", "install",
		"installer", "joymouse", "js3", "pkunzip", "readme", "register", "restore", "setup", "setsound",
		"uninstall", "update":
		return true
	default:
		return false
	}
}

// inferDOSBatchTerminalTargets keeps a named launcher batch as the default unless it demonstrably opens a known
// interactive helper before ending in another scanned EXE/COM. In that narrow case the terminal program is the
// safer zero-interaction default; the batch remains available for an administrator who needs its setup flow.
func inferDOSBatchTerminalTargets(entries []preparedDOSEntry) {
	byPath := make(map[string]int, len(entries))
	for index := range entries {
		entries[index].inferredTerminalTarget = false
		byPath[strings.ToLower(entries[index].path)] = index
	}
	for index := range entries {
		launcher := entries[index]
		if launcher.kind != "BAT" || len(launcher.batchContents) == 0 {
			continue
		}
		launcherName := normalizedDOSProgramName(launcher.path)
		if !preferredDOSProgramName(launcherName) {
			continue
		}
		invocations := resolveDOSBatchInvocations(launcher.path, launcher.batchContents, entries, byPath)
		if len(invocations) < 2 {
			continue
		}
		terminal := invocations[len(invocations)-1]
		terminalName := normalizedDOSProgramName(entries[terminal].path)
		if terminal == index || entries[terminal].kind == "BAT" || helperDOSProgramName(terminalName) {
			continue
		}
		interactiveHelper := false
		for _, invoked := range invocations[:len(invocations)-1] {
			if helperDOSProgramName(normalizedDOSProgramName(entries[invoked].path)) {
				interactiveHelper = true
				break
			}
		}
		if interactiveHelper {
			entries[terminal].inferredTerminalTarget = true
		}
	}
}

func normalizedDOSProgramName(programPath string) string {
	extension := filepath.Ext(programPath)
	base := strings.TrimSuffix(filepath.Base(programPath), extension)
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, base)
}

func resolveDOSBatchInvocations(
	launcherPath string,
	contents []byte,
	entries []preparedDOSEntry,
	byPath map[string]int,
) []int {
	if len(contents) > maxDOSBatchInspectionBytes {
		return nil
	}
	directory := path.Dir(strings.ReplaceAll(launcherPath, "\\", "/"))
	invocations := make([]int, 0, 4)
	for _, line := range strings.FieldsFunc(string(contents), func(character rune) bool {
		return character == '\r' || character == '\n'
	}) {
		token, safe := dosBatchCommandToken(line)
		if !safe {
			return nil
		}
		if token == "" {
			continue
		}
		invoked, matched := resolveDOSBatchProgram(token, directory, entries, byPath)
		if !matched {
			return nil
		}
		invocations = append(invocations, invoked)
	}
	return invocations
}

func dosBatchCommandToken(line string) (string, bool) {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "@")
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return "", true
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", true
	}
	first := strings.ToLower(strings.Trim(fields[0], "\""))
	if ignoredDOSBatchCommand(first) {
		return "", true
	}
	if conditionalDOSBatchCommand(first) {
		return "", false
	}
	if first == "call" {
		if len(fields) < 2 {
			return "", false
		}
		return strings.Trim(fields[1], "\""), true
	}
	return strings.Trim(fields[0], "\""), true
}

func ignoredDOSBatchCommand(command string) bool {
	switch command {
	case "rem", "echo", "set", "cd", "chdir", "path", "prompt":
		return true
	default:
		return strings.HasPrefix(command, "::")
	}
}

func conditionalDOSBatchCommand(command string) bool {
	switch command {
	case "goto", "if", "for":
		return true
	default:
		return false
	}
}

func resolveDOSBatchProgram(
	token, directory string,
	entries []preparedDOSEntry,
	byPath map[string]int,
) (int, bool) {
	normalized := strings.ReplaceAll(token, "\\", "/")
	if len(normalized) >= 2 && normalized[1] == ':' {
		normalized = normalized[2:]
	}
	normalized = strings.TrimLeft(normalized, "/")
	if normalized == "" {
		return 0, false
	}
	if !strings.Contains(normalized, "/") && directory != "." {
		normalized = path.Join(directory, normalized)
	}
	candidates := []string{normalized}
	if filepath.Ext(normalized) == "" {
		candidates = []string{normalized + ".EXE", normalized + ".COM", normalized + ".BAT"}
	}
	for _, candidate := range candidates {
		index, matched := byPath[strings.ToLower(path.Clean(candidate))]
		if matched && index >= 0 && index < len(entries) {
			return index, true
		}
	}
	return 0, false
}

func (service *Service) inspectDOSBatch(digest string) []byte {
	if service.blobs == nil || digest == "" {
		return nil
	}
	file, err := service.blobs.OpenDigest(digest)
	if err != nil {
		return nil
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(file, maxDOSBatchInspectionBytes+1))
	if err != nil || len(contents) > maxDOSBatchInspectionBytes {
		return nil
	}
	return contents
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

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) prepareDOSFiles(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	dispositions, candidates := partitionDOSFiles(files)
	if sourceType == "FILES" {
		return service.prepareDOSArchive(ctx, dispositions, candidates)
	}
	return service.prepareDOSDirectory(dispositions, candidates)
}

func partitionDOSFiles(files []importSourceFile) ([]preparedDisposition, []importSourceFile) {
	dispositions := make([]preparedDisposition, 0, len(files))
	candidates := make([]importSourceFile, 0, len(files))
	for _, file := range files {
		if knownSidecar(file.path) {
			dispositions = append(dispositions, ignoredDisposition(file))
		} else {
			candidates = append(candidates, file)
		}
	}
	return dispositions, candidates
}

func (service *Service) prepareDOSArchive(
	ctx context.Context,
	dispositions []preparedDisposition,
	candidates []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	if len(candidates) != 1 || !strings.EqualFold(filepath.Ext(candidates[0].path), ".zip") ||
		service.blobs == nil {
		return appendRejectedDOSFiles(dispositions, candidates, "AMBIGUOUS_DOS_BUNDLE"), nil, nil
	}
	file := candidates[0]
	entries, err := importing.ScanZIP(ctx, service.blobs.Path(file.sha256), importing.DOSArchiveLimits())
	if err != nil {
		return append(dispositions, rejectedDisposition(file, archiveReason(err))), nil, nil
	}
	sources, programs, materialized, err := service.materializeDOSArchive(ctx, file, entries)
	if err != nil {
		return append(dispositions, rejectedDisposition(file, archiveReason(err))), nil, nil
	}
	if len(programs) == 0 {
		return append(dispositions, rejectedDisposition(file, "NO_DOS_PROGRAM")), nil, nil
	}
	rankDOSEntries(programs)
	dispositions = append(dispositions, sourceDisposition(file))
	group := preparedGroup{
		sources: sources, dosEntries: programs, defaultDOSEntry: programs[0].path,
		bundleBlobID: file.blobID, titleSource: filepath.Base(file.path),
	}
	archive := preparedArchive{blobID: file.blobID, entries: entries, materialized: materialized}
	return dispositions, []preparedGroup{group}, []preparedArchive{archive}
}

func appendRejectedDOSFiles(
	dispositions []preparedDisposition,
	files []importSourceFile,
	reason string,
) []preparedDisposition {
	for _, file := range files {
		dispositions = append(dispositions, rejectedDisposition(file, reason))
	}
	return dispositions
}

func (service *Service) materializeDOSArchive(
	ctx context.Context,
	file importSourceFile,
	entries []importing.ArchiveEntry,
) ([]preparedSource, []preparedDOSEntry, map[int]blobstore.Metadata, error) {
	programs := make([]preparedDOSEntry, 0)
	sources := make([]preparedSource, 0, len(entries))
	materialized := make(map[int]blobstore.Metadata, len(entries))
	for _, entry := range entries {
		metadata, err := service.materializeArchiveEntry(ctx, service.blobs.Path(file.sha256), entry)
		if err != nil {
			return nil, nil, nil, err
		}
		ordinal := entry.Ordinal
		materialized[ordinal] = metadata
		sources = append(sources, preparedSource{
			file: file, role: "DOS_SOURCE", logicalName: entry.NormalizedPath,
			archiveBlobID: file.blobID, archiveOrdinal: &ordinal,
		})
		if program, ok := service.preparedDOSProgram(entry.NormalizedPath, metadata.SHA256, len(programs)); ok {
			programs = append(programs, program)
		}
	}
	return sources, programs, materialized, nil
}

func (service *Service) preparedDOSProgram(filePath, digest string, rank int) (preparedDOSEntry, bool) {
	kind, ok := dosProgram(filePath)
	if !ok {
		return preparedDOSEntry{}, false
	}
	var batchContents []byte
	if kind == "BAT" {
		batchContents = service.inspectDOSBatch(digest)
	}
	return preparedDOSEntry{
		path: filePath, kind: kind, rank: rank, safe: directDOSPathSafe(filePath),
		batchContents: batchContents,
	}, true
}

func (service *Service) prepareDOSDirectory(
	dispositions []preparedDisposition,
	candidates []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	if len(candidates) == 0 || service.blobs == nil {
		return dispositions, nil, nil
	}
	sources, programs := service.collectDOSDirectoryFiles(candidates)
	for _, file := range candidates {
		dispositions = append(dispositions, sourceDisposition(file))
	}
	if len(programs) == 0 {
		return rejectSourceDispositions(dispositions, "NO_DOS_PROGRAM"), nil, nil
	}
	rankDOSEntries(programs)
	bundle, err := service.bundleDOSDirectory(candidates)
	if err != nil {
		return rejectSourceDispositions(dispositions, "DOS_BUNDLE_FAILED"), nil, nil
	}
	group := preparedGroup{
		sources: sources, dosEntries: programs, defaultDOSEntry: programs[0].path,
		bundle: &bundle, titleSource: dosDirectoryTitle(candidates),
	}
	return dispositions, []preparedGroup{group}, nil
}

func (service *Service) collectDOSDirectoryFiles(
	candidates []importSourceFile,
) ([]preparedSource, []preparedDOSEntry) {
	programs := make([]preparedDOSEntry, 0)
	sources := make([]preparedSource, 0, len(candidates))
	for _, file := range candidates {
		sources = append(sources, preparedSource{file: file, role: "DOS_SOURCE", logicalName: file.path})
		if program, ok := service.preparedDOSProgram(file.path, file.sha256, len(programs)); ok {
			programs = append(programs, program)
		}
	}
	return sources, programs
}

func rejectSourceDispositions(dispositions []preparedDisposition, reason string) []preparedDisposition {
	for index := range dispositions {
		if dispositions[index].disposition == "SOURCE" {
			dispositions[index].disposition = "REJECTED"
			dispositions[index].reason = reason
		}
	}
	return dispositions
}
