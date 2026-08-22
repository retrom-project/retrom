package libraryimport

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
	"retrom/internal/corevalidation"
	"retrom/internal/importing"
	"retrom/internal/multidisc"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) prepareImportFiles(
	ctx context.Context,
	platformID, sourceType string,
	files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	switch platformID {
	case "dos":
		return service.prepareDOSFiles(ctx, sourceType, files)
	case "arcade":
		return service.prepareArcadeFiles(ctx, files, datID)
	}
	profile, exists := contentprofile.ByPlatform(platformID)
	if !exists {
		return prepareUnsupportedPlatformFiles(files)
	}
	return service.prepareProfileFiles(ctx, platformID, profile, files)
}

func prepareUnsupportedPlatformFiles(
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	dispositions := make([]preparedDisposition, 0, len(files))
	for _, file := range files {
		disposition := preparedDisposition{
			file: file, disposition: "REJECTED", reason: "UNSUPPORTED_CONTENT_FORMAT",
		}
		if knownSidecar(file.path) {
			disposition.disposition = "IGNORED"
			disposition.reason = "IGNORED_SYSTEM_SIDECAR"
		}
		dispositions = append(dispositions, disposition)
	}
	return dispositions, nil, nil
}

func (service *Service) prepareProfileFiles(
	ctx context.Context,
	platformID string,
	profile contentprofile.Profile,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	dispositions := make([]preparedDisposition, 0, len(files))
	groups := make([]preparedGroup, 0, len(files))
	archives := make([]preparedArchive, 0)
	for _, file := range files {
		disposition, group, archive := service.prepareProfileFile(ctx, platformID, profile, file)
		dispositions = append(dispositions, disposition)
		if group != nil {
			groups = append(groups, *group)
		}
		if archive != nil {
			archives = append(archives, *archive)
		}
	}
	return dispositions, groups, archives
}

func (service *Service) prepareProfileFile(
	ctx context.Context,
	platformID string,
	profile contentprofile.Profile,
	file importSourceFile,
) (preparedDisposition, *preparedGroup, *preparedArchive) {
	if knownSidecar(file.path) {
		return ignoredDisposition(file), nil, nil
	}
	if contentprofile.AcceptsRaw(platformID, file.path) {
		return sourceDisposition(file), singleSourceGroup(file, filepath.Base(file.path)), nil
	}
	archiveFormat, reason := profileArchiveFormat(file.path)
	if reason != "" || service.blobs == nil ||
		profile.ArchivePolicy != contentprofile.ArchiveSinglePrimary ||
		!contentprofile.AcceptsArchive(platformID, archiveFormat) {
		return rejectedDisposition(file, reasonOrUnsupported(reason)), nil, nil
	}
	entries, err := service.scanProfileArchive(ctx, file, archiveFormat)
	if err != nil {
		return rejectedDisposition(file, archiveReason(err)), nil, nil
	}
	candidate, err := contentprofile.SelectArchivePrimary(platformID, entries)
	if err != nil {
		return rejectedDisposition(file, archiveSelectionReason(err)), nil, nil
	}
	selected, err := service.materializeArchiveEntry(ctx, service.blobs.Path(file.sha256), candidate)
	if err != nil {
		return rejectedDisposition(file, archiveReason(err)), nil, nil
	}
	ordinal := candidate.Ordinal
	group := &preparedGroup{sources: []preparedSource{{
		file: file, role: "CONTENT", logicalName: filepath.Base(candidate.NormalizedPath),
		archiveBlobID: file.blobID, archiveOrdinal: &ordinal,
	}}}
	archive := &preparedArchive{
		blobID: file.blobID, entries: entries,
		materialized: map[int]blobstore.Metadata{ordinal: selected},
	}
	return sourceDisposition(file), group, archive
}

func ignoredDisposition(file importSourceFile) preparedDisposition {
	return preparedDisposition{file: file, disposition: "IGNORED", reason: "IGNORED_SYSTEM_SIDECAR"}
}

func sourceDisposition(file importSourceFile) preparedDisposition {
	return preparedDisposition{file: file, disposition: "SOURCE"}
}

func rejectedDisposition(file importSourceFile, reason string) preparedDisposition {
	return preparedDisposition{file: file, disposition: "REJECTED", reason: reason}
}

func singleSourceGroup(file importSourceFile, logicalName string) *preparedGroup {
	return &preparedGroup{sources: []preparedSource{{file: file, role: "CONTENT", logicalName: logicalName}}}
}

func profileArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	extension := strings.ToLower(filepath.Ext(filePath))
	switch {
	case extension == ".zip":
		return contentprofile.ArchiveZIP, ""
	case extension == ".7z":
		return contentprofile.ArchiveSevenZip, ""
	case strings.HasSuffix(strings.ToLower(filePath), ".7z.001"):
		return "", "ARCHIVE_VOLUME_UNSUPPORTED"
	default:
		return "", "UNSUPPORTED_CONTENT_FORMAT"
	}
}

func reasonOrUnsupported(reason string) string {
	if reason == "" {
		return "UNSUPPORTED_CONTENT_FORMAT"
	}
	return reason
}

func archiveSelectionReason(err error) string {
	switch {
	case errors.Is(err, contentprofile.ErrNoSupportedContent):
		return "NO_SUPPORTED_CONTENT"
	case errors.Is(err, contentprofile.ErrAmbiguousPrimaryContent):
		return "AMBIGUOUS_PRIMARY_CONTENT"
	default:
		return archiveReason(err)
	}
}

func (service *Service) scanProfileArchive(
	ctx context.Context,
	file importSourceFile,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, error) {
	archivePath := service.blobs.Path(file.sha256)
	var entries []importing.ArchiveEntry
	var err error
	if archiveFormat == contentprofile.ArchiveZIP {
		entries, err = importing.ScanZIP(ctx, archivePath, importing.DefaultArchiveLimits())
	} else {
		entries, err = importing.ScanSevenZip(ctx, archivePath, importing.DefaultArchiveLimits())
	}
	if err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	return entries, nil
}

func (service *Service) readMultiDiscBlob(file importSourceFile, maximum int64) ([]byte, error) {
	if service.blobs == nil || file.size > maximum {
		return nil, ErrInvalid
	}
	reader, err := service.blobs.OpenDigest(file.sha256)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil || int64(len(contents)) != file.size || int64(len(contents)) > maximum {
		return nil, ErrInvalid
	}
	return contents, nil
}

func (service *Service) readMultiDiscHeader(file importSourceFile) ([]byte, error) {
	if service.blobs == nil || file.size < 8 {
		return nil, ErrInvalid
	}
	reader, err := service.blobs.OpenDigest(file.sha256)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	header := make([]byte, 8)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, ErrInvalid
	}
	return header, nil
}

func multiDiscFileBuckets(
	files []importSourceFile,
) (map[string][]importSourceFile, map[string][]importSourceFile, int) {
	playlistsByDirectory := make(map[string][]importSourceFile)
	filesByDirectory := make(map[string][]importSourceFile)
	playlistCount := 0
	for _, file := range files {
		directory := path.Dir(file.path)
		filesByDirectory[directory] = append(filesByDirectory[directory], file)
		if !knownSidecar(file.path) && multidisc.ASCIIFold(path.Ext(file.path)) == ".m3u" {
			playlistsByDirectory[directory] = append(playlistsByDirectory[directory], file)
			playlistCount++
		}
	}
	return playlistsByDirectory, filesByDirectory, playlistCount
}

func initialMultiDiscDispositions(files []importSourceFile) map[string]preparedDisposition {
	dispositionByID := make(map[string]preparedDisposition, len(files))
	for _, file := range files {
		reason := "NOT_REFERENCED_BY_PLAYLIST"
		if knownSidecar(file.path) {
			reason = "IGNORED_SYSTEM_SIDECAR"
		}
		dispositionByID[file.id] = preparedDisposition{file: file, disposition: "IGNORED", reason: reason}
	}
	return dispositionByID
}

func (service *Service) multiDiscCandidates(
	files []importSourceFile,
	playlistID string,
) ([]multidisc.File, error) {
	candidates := make([]multidisc.File, 0, len(files))
	for _, file := range files {
		if file.id == playlistID || knownSidecar(file.path) {
			continue
		}
		var header []byte
		var err error
		if multidisc.ASCIIFold(path.Ext(file.path)) == ".chd" && file.size >= 8 {
			header, err = service.readMultiDiscHeader(file)
			if err != nil {
				return nil, err
			}
		}
		candidates = append(candidates, multidisc.File{
			Basename: path.Base(file.path), LogicalName: path.Base(file.path),
			UploadFileID: file.id, BlobID: file.blobID, BlobSHA256: file.sha256,
			SizeBytes: file.size, Header: header,
		})
	}
	return candidates, nil
}

func multiDiscParseReason(err error) string {
	var validationError *multidisc.ValidationError
	if errors.As(err, &validationError) {
		return string(validationError.Code)
	}
	return "MULTI_DISC_PLAYLIST_INVALID"
}

func preparedMultiDiscGroup(
	directory string,
	playlist importSourceFile,
	parsed multidisc.Result,
	canonical blobstore.Metadata,
) (preparedGroup, []preparedDisposition, error) {
	playlistOrder := 0
	group := preparedGroup{
		contentKind: multidisc.ContentKind, titleSource: path.Base(playlist.path),
		sources: []preparedSource{{
			file: playlist, role: "PLAYLIST_SOURCE", logicalName: path.Base(playlist.path),
			sortOrder: &playlistOrder,
		}},
		canonicalPlaylist: &canonical,
	}
	var err error
	group.groupKey, err = multidisc.GroupKey(directory, playlist.sha256)
	if err != nil {
		return preparedGroup{}, nil, ErrInvalid
	}
	missing := make([]corevalidation.MultiDiscMissingEntry, 0)
	dispositions := []preparedDisposition{{file: playlist, disposition: "SOURCE"}}
	for _, entry := range parsed.Entries {
		preparedEntry := preparedMultiDiscEntry{
			ordinal: entry.Ordinal, state: string(entry.State), sourceReference: entry.SourceReference,
			normalizedReference: entry.NormalizedReference, canonicalName: entry.CanonicalName,
		}
		if entry.State == multidisc.EntryPresent {
			discOrder := entry.Ordinal
			preparedEntry.uploadFileID = entry.File.UploadFileID
			preparedEntry.blobID = entry.File.BlobID
			preparedEntry.sourceLogicalName = entry.File.LogicalName
			sourceFile := importSourceFile{
				id: entry.File.UploadFileID, path: path.Join(directory, entry.File.Basename),
				blobID: entry.File.BlobID, sha256: entry.File.BlobSHA256, size: entry.File.SizeBytes,
			}
			group.sources = append(group.sources, preparedSource{
				file: sourceFile, role: "DISC", logicalName: entry.File.LogicalName, sortOrder: &discOrder,
			})
			dispositions = append(dispositions, preparedDisposition{file: sourceFile, disposition: "SOURCE"})
		} else {
			missing = append(missing, corevalidation.MultiDiscMissingEntry{
				Ordinal: entry.Ordinal, SourceReference: entry.SourceReference,
				NormalizedReference: entry.NormalizedReference,
			})
		}
		group.multiEntries = append(group.multiEntries, preparedEntry)
	}
	group.validationStatus, group.compatibilityCode = "READY", "READY"
	if len(missing) > 0 {
		group.validationStatus, group.compatibilityCode = "BLOCKED", "MULTI_DISC_FILE_MISSING"
	}
	group.multiDependency = &corevalidation.MultiDiscSnapshot{
		DiscCount: len(parsed.Entries), MissingEntries: missing,
	}
	if len(missing) == 0 {
		group.multiDependency.ContentKind = corevalidation.MultiDiscContentKind
		group.multiDependency.ParserVersion = corevalidation.MultiDiscParserVersion
		group.multiDependency.Delivery = corevalidation.MultiDiscDelivery
		group.multiDependency.CanonicalPlaylistSHA256 = canonical.SHA256
		group.multiDependency.OrderedDiscSHA256 = make([]string, 0, len(parsed.Entries))
		for _, entry := range parsed.Entries {
			group.multiDependency.OrderedDiscSHA256 = append(
				group.multiDependency.OrderedDiscSHA256, entry.File.BlobSHA256,
			)
		}
	}
	return group, dispositions, nil
}

func (service *Service) prepareMultiDiscDirectory(
	directory string,
	files []importSourceFile,
	playlist importSourceFile,
	limits contentcapability.MultiDiscLimits,
) (preparedGroup, []preparedDisposition, error) {
	playlistBytes, err := service.readMultiDiscBlob(playlist, multidisc.MaxPlaylistBytes)
	if err != nil {
		return preparedGroup{}, nil, err
	}
	candidates, err := service.multiDiscCandidates(files, playlist.id)
	if err != nil {
		return preparedGroup{}, nil, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: limits.MaxDiscs, MaxTotalBytes: limits.MaxTotalBytes,
	})
	if err != nil {
		return preparedGroup{}, []preparedDisposition{{
			file: playlist, disposition: "REJECTED", reason: multiDiscParseReason(err),
		}}, nil
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return preparedGroup{}, nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	return preparedMultiDiscGroup(directory, playlist, parsed, canonical)
}

func (service *Service) prepareMultiDiscFiles(
	files []importSourceFile,
	limits contentcapability.MultiDiscLimits,
) ([]preparedDisposition, []preparedGroup, error) {
	playlistsByDirectory, filesByDirectory, playlistCount := multiDiscFileBuckets(files)
	if playlistCount == 0 {
		return nil, nil, ErrMultiDiscPlaylistMissing
	}
	directories := make([]string, 0, len(playlistsByDirectory))
	for directory := range playlistsByDirectory {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	dispositionByID := initialMultiDiscDispositions(files)
	groups := make([]preparedGroup, 0, len(directories))
	for _, directory := range directories {
		playlists := playlistsByDirectory[directory]
		if len(playlists) > 1 {
			for _, playlist := range playlists {
				dispositionByID[playlist.id] = preparedDisposition{
					file: playlist, disposition: "REJECTED", reason: "MULTI_DISC_PLAYLIST_AMBIGUOUS",
				}
			}
			continue
		}
		group, resolved, err := service.prepareMultiDiscDirectory(
			directory, filesByDirectory[directory], playlists[0], limits,
		)
		if err != nil {
			return nil, nil, err
		}
		for _, disposition := range resolved {
			dispositionByID[disposition.file.id] = disposition
		}
		if len(group.sources) > 0 {
			groups = append(groups, group)
		}
	}
	dispositions := make([]preparedDisposition, 0, len(files))
	for _, file := range files {
		dispositions = append(dispositions, dispositionByID[file.id])
	}
	return dispositions, groups, nil
}
