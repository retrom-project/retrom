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

	application "retrom/internal/service/libraryimport"

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
			File: file, Disposition: "REJECTED", Reason: "UNSUPPORTED_CONTENT_FORMAT",
		}
		if knownSidecar(file.Path) {
			disposition.Disposition = "IGNORED"
			disposition.Reason = "IGNORED_SYSTEM_SIDECAR"
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
	if knownSidecar(file.Path) {
		return ignoredDisposition(file), nil, nil
	}
	if contentprofile.AcceptsRaw(platformID, file.Path) {
		return sourceDisposition(file), singleSourceGroup(file, filepath.Base(file.Path)), nil
	}
	archiveFormat, reason := profileArchiveFormat(file.Path)
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
	selected, err := service.materializeArchiveEntry(ctx, service.blobs.Path(file.SHA256), candidate)
	if err != nil {
		return rejectedDisposition(file, archiveReason(err)), nil, nil
	}
	ordinal := candidate.Ordinal
	group := &preparedGroup{Sources: []preparedSource{{
		File: file, Role: "CONTENT", LogicalName: filepath.Base(candidate.NormalizedPath),
		ArchiveBlobID: file.BlobID, ArchiveOrdinal: &ordinal,
	}}}
	archive := &preparedArchive{
		BlobID: file.BlobID, Entries: entries,
		Materialized: map[int]blobstore.Metadata{ordinal: selected},
	}
	return sourceDisposition(file), group, archive
}

func ignoredDisposition(file importSourceFile) preparedDisposition {
	return preparedDisposition{File: file, Disposition: "IGNORED", Reason: "IGNORED_SYSTEM_SIDECAR"}
}

func sourceDisposition(file importSourceFile) preparedDisposition {
	return preparedDisposition{File: file, Disposition: "SOURCE"}
}

func rejectedDisposition(file importSourceFile, reason string) preparedDisposition {
	return preparedDisposition{File: file, Disposition: "REJECTED", Reason: reason}
}

func singleSourceGroup(file importSourceFile, logicalName string) *preparedGroup {
	return &preparedGroup{Sources: []preparedSource{{File: file, Role: "CONTENT", LogicalName: logicalName}}}
}

func profileArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	return application.ImportArchiveFormat(filePath)
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
	archivePath := service.blobs.Path(file.SHA256)
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
	if service.blobs == nil || file.Size > maximum {
		return nil, ErrInvalid
	}
	reader, err := service.blobs.OpenDigest(file.SHA256)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil || int64(len(contents)) != file.Size || int64(len(contents)) > maximum {
		return nil, ErrInvalid
	}
	return contents, nil
}

func (service *Service) readMultiDiscHeader(file importSourceFile) ([]byte, error) {
	if service.blobs == nil || file.Size < 8 {
		return nil, ErrInvalid
	}
	reader, err := service.blobs.OpenDigest(file.SHA256)
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
		directory := path.Dir(file.Path)
		filesByDirectory[directory] = append(filesByDirectory[directory], file)
		if !knownSidecar(file.Path) && multidisc.ASCIIFold(path.Ext(file.Path)) == ".m3u" {
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
		if knownSidecar(file.Path) {
			reason = "IGNORED_SYSTEM_SIDECAR"
		}
		dispositionByID[file.ID] = preparedDisposition{File: file, Disposition: "IGNORED", Reason: reason}
	}
	return dispositionByID
}

func (service *Service) multiDiscCandidates(
	files []importSourceFile,
	playlistID string,
) ([]multidisc.File, error) {
	candidates := make([]multidisc.File, 0, len(files))
	for _, file := range files {
		if file.ID == playlistID || knownSidecar(file.Path) {
			continue
		}
		var header []byte
		var err error
		if multidisc.ASCIIFold(path.Ext(file.Path)) == ".chd" && file.Size >= 8 {
			header, err = service.readMultiDiscHeader(file)
			if err != nil {
				return nil, err
			}
		}
		candidates = append(candidates, multidisc.File{
			Basename: path.Base(file.Path), LogicalName: path.Base(file.Path),
			UploadFileID: file.ID, BlobID: file.BlobID, BlobSHA256: file.SHA256,
			SizeBytes: file.Size, Header: header,
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
		ContentKind: multidisc.ContentKind, TitleSource: path.Base(playlist.Path),
		Sources: []preparedSource{{
			File: playlist, Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.Path),
			SortOrder: &playlistOrder,
		}},
		CanonicalPlaylist: &canonical,
	}
	var err error
	group.GroupKey, err = multidisc.GroupKey(directory, playlist.SHA256)
	if err != nil {
		return preparedGroup{}, nil, ErrInvalid
	}
	missing := make([]corevalidation.MultiDiscMissingEntry, 0)
	dispositions := []preparedDisposition{{File: playlist, Disposition: "SOURCE"}}
	for _, entry := range parsed.Entries {
		preparedEntry := preparedMultiDiscEntry{
			Ordinal: entry.Ordinal, State: string(entry.State), SourceReference: entry.SourceReference,
			NormalizedReference: entry.NormalizedReference, CanonicalName: entry.CanonicalName,
		}
		if entry.State == multidisc.EntryPresent {
			discOrder := entry.Ordinal
			preparedEntry.UploadFileID = entry.File.UploadFileID
			preparedEntry.BlobID = entry.File.BlobID
			preparedEntry.SourceLogicalName = entry.File.LogicalName
			sourceFile := importSourceFile{
				ID: entry.File.UploadFileID, Path: path.Join(directory, entry.File.Basename),
				BlobID: entry.File.BlobID, SHA256: entry.File.BlobSHA256, Size: entry.File.SizeBytes,
			}
			group.Sources = append(group.Sources, preparedSource{
				File: sourceFile, Role: "DISC", LogicalName: entry.File.LogicalName, SortOrder: &discOrder,
			})
			dispositions = append(dispositions, preparedDisposition{File: sourceFile, Disposition: "SOURCE"})
		} else {
			missing = append(missing, corevalidation.MultiDiscMissingEntry{
				Ordinal: entry.Ordinal, SourceReference: entry.SourceReference,
				NormalizedReference: entry.NormalizedReference,
			})
		}
		group.MultiEntries = append(group.MultiEntries, preparedEntry)
	}
	group.ValidationStatus, group.CompatibilityCode = "READY", "READY"
	if len(missing) > 0 {
		group.ValidationStatus, group.CompatibilityCode = "BLOCKED", "MULTI_DISC_FILE_MISSING"
	}
	group.MultiDependency = &corevalidation.MultiDiscSnapshot{
		DiscCount: len(parsed.Entries), MissingEntries: missing,
	}
	if len(missing) == 0 {
		group.MultiDependency.ContentKind = corevalidation.MultiDiscContentKind
		group.MultiDependency.ParserVersion = corevalidation.MultiDiscParserVersion
		group.MultiDependency.Delivery = corevalidation.MultiDiscDelivery
		group.MultiDependency.CanonicalPlaylistSHA256 = canonical.SHA256
		group.MultiDependency.OrderedDiscSHA256 = make([]string, 0, len(parsed.Entries))
		for _, entry := range parsed.Entries {
			group.MultiDependency.OrderedDiscSHA256 = append(
				group.MultiDependency.OrderedDiscSHA256, entry.File.BlobSHA256,
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
	candidates, err := service.multiDiscCandidates(files, playlist.ID)
	if err != nil {
		return preparedGroup{}, nil, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: limits.MaxDiscs, MaxTotalBytes: limits.MaxTotalBytes,
	})
	if err != nil {
		return preparedGroup{}, []preparedDisposition{{
			File: playlist, Disposition: "REJECTED", Reason: multiDiscParseReason(err),
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
				dispositionByID[playlist.ID] = preparedDisposition{
					File: playlist, Disposition: "REJECTED", Reason: "MULTI_DISC_PLAYLIST_AMBIGUOUS",
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
			dispositionByID[disposition.File.ID] = disposition
		}
		if len(group.Sources) > 0 {
			groups = append(groups, group)
		}
	}
	dispositions := make([]preparedDisposition, 0, len(files))
	for _, file := range files {
		dispositions = append(dispositions, dispositionByID[file.ID])
	}
	return dispositions, groups, nil
}
