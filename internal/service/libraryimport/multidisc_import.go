package libraryimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"

	"retrom/internal/cleanup"
	contentcapability "retrom/internal/content/capability"
	"retrom/internal/content/diagnostic"
	contentprepare "retrom/internal/content/prepare"
	contentprofile "retrom/internal/content/profile"
	corevalidation "retrom/internal/core/validation"
	"retrom/internal/filestore"
	"retrom/internal/multidisc"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *ImportPreparation) PrepareImportFiles(
	ctx context.Context, platformID, sourceType string, files []ImportFile, datID string,
) ([]PreparedDisposition, []PreparedGroup, []PreparedArchive, error) {
	if platformID == "arcade" {
		return service.PrepareArcadeFiles(ctx, files, datID)
	}
	var dispositions []PreparedDisposition
	var groups []PreparedGroup
	var archives []PreparedArchive
	if platformID == "dos" {
		dispositions, groups, archives = service.PrepareDOSFiles(ctx, sourceType, files)
	} else {
		_, exists := contentprofile.ByPlatform(platformID)
		if exists {
			dispositions, groups, archives = service.prepareProfileFiles(ctx, platformID, files)
		} else {
			dispositions = prepareUnsupportedPlatformFiles(files)
		}
	}
	return dispositions, groups, archives, nil
}

func prepareUnsupportedPlatformFiles(
	files []ImportFile,
) []PreparedDisposition {
	dispositions := make([]PreparedDisposition, 0, len(files))
	for _, file := range files {
		disposition := PreparedDisposition{
			File: file, Disposition: "REJECTED", Reason: "UNSUPPORTED_CONTENT_FORMAT",
		}
		if knownSidecar(file.Path) {
			disposition.Disposition = "IGNORED"
			disposition.Reason = "IGNORED_SYSTEM_SIDECAR"
		}
		dispositions = append(dispositions, disposition)
	}
	return dispositions
}

func (service *ImportPreparation) prepareProfileFiles(
	ctx context.Context,
	platformID string,
	files []ImportFile,
) ([]PreparedDisposition, []PreparedGroup, []PreparedArchive) {
	dispositions := make([]PreparedDisposition, 0, len(files))
	groups := make([]PreparedGroup, 0, len(files))
	archives := make([]PreparedArchive, 0)
	for _, file := range files {
		disposition, group, archive := service.prepareProfileFile(ctx, platformID, file)
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

func (service *ImportPreparation) prepareProfileFile(
	ctx context.Context,
	platformID string,
	file ImportFile,
) (PreparedDisposition, *PreparedGroup, *PreparedArchive) {
	if knownSidecar(file.Path) {
		return ignoredDisposition(file), nil, nil
	}
	result, err := contentprepare.New(service.blobs).Single(ctx, platformID, contentprepare.File{
		LogicalName: file.Path, Record: file.FileRecord, Size: file.Size,
	})
	if err != nil {
		var invalid *contentprepare.Invalid
		if errors.As(err, &invalid) {
			return rejectedDisposition(file, invalid.Code), nil, nil
		}
		return rejectedDisposition(file, ArchiveReason(err)), nil, nil
	}
	if result.Selected == nil {
		return sourceDisposition(file), singleSourceGroup(file, filepath.Base(file.Path)), nil
	}
	ordinal := result.Selected.Ordinal
	group := &PreparedGroup{Sources: []PreparedSource{{
		File: file, Role: "CONTENT", LogicalName: result.File.LogicalName,
		ArchiveFileRecord: file.FileRecord, ArchiveOrdinal: &ordinal,
	}}}
	archive := &PreparedArchive{
		FileRecord: file.FileRecord, Entries: result.ArchiveEntries,
		Materialized: map[int]filestore.Metadata{ordinal: *result.Materialized},
	}
	return sourceDisposition(file), group, archive
}

func ignoredDisposition(file ImportFile) PreparedDisposition {
	return PreparedDisposition{File: file, Disposition: "IGNORED", Reason: "IGNORED_SYSTEM_SIDECAR"}
}

func sourceDisposition(file ImportFile) PreparedDisposition {
	return PreparedDisposition{File: file, Disposition: "SOURCE"}
}

func rejectedDisposition(file ImportFile, reason string) PreparedDisposition {
	return PreparedDisposition{File: file, Disposition: "REJECTED", Reason: reason}
}

func singleSourceGroup(file ImportFile, logicalName string) *PreparedGroup {
	return &PreparedGroup{Sources: []PreparedSource{{File: file, Role: "CONTENT", LogicalName: logicalName}}}
}

func profileArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	return ImportArchiveFormat(filePath)
}

func (service *ImportPreparation) readMultiDiscBlob(file ImportFile, maximum int64) ([]byte, error) {
	if service.blobs == nil || file.Size > maximum {
		return nil, ErrInvalid
	}
	reader, err := service.blobs.OpenRecord(file.FileRecord)
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

func (service *ImportPreparation) readMultiDiscHeader(file ImportFile) ([]byte, error) {
	if service.blobs == nil || file.Size < 8 {
		return nil, ErrInvalid
	}
	reader, err := service.blobs.OpenRecord(file.FileRecord)
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
	files []ImportFile,
) (map[string][]ImportFile, map[string][]ImportFile, int) {
	playlistsByDirectory := make(map[string][]ImportFile)
	filesByDirectory := make(map[string][]ImportFile)
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

func initialMultiDiscDispositions(files []ImportFile) map[string]PreparedDisposition {
	dispositionByID := make(map[string]PreparedDisposition, len(files))
	for _, file := range files {
		reason := "NOT_REFERENCED_BY_PLAYLIST"
		if knownSidecar(file.Path) {
			reason = "IGNORED_SYSTEM_SIDECAR"
		}
		dispositionByID[file.ID] = PreparedDisposition{File: file, Disposition: "IGNORED", Reason: reason}
	}
	return dispositionByID
}

func (service *ImportPreparation) multiDiscCandidates(
	files []ImportFile,
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
			UploadFileID: file.ID, FileRecord: file.FileRecord, BlobSHA256: file.SHA256,
			SizeBytes: file.Size, Header: header,
		})
	}
	return candidates, nil
}

func multiDiscRejection(err error, relativePath string) *diagnostic.Rejection {
	result := &diagnostic.Rejection{Code: "MULTI_DISC_PLAYLIST_INVALID", RelativePath: relativePath}
	var validationError *multidisc.ValidationError
	if errors.As(err, &validationError) {
		result.Code, result.Limit = string(validationError.Code), validationError.Limit
	}
	return result
}

func preparedMultiDiscGroup(
	directory string,
	playlist ImportFile,
	parsed multidisc.Result,
	canonical filestore.Metadata,
) (PreparedGroup, []PreparedDisposition, error) {
	playlistOrder := 0
	group := PreparedGroup{
		ContentKind: multidisc.ContentKind, TitleSource: path.Base(playlist.Path),
		Sources: []PreparedSource{{
			File: playlist, Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.Path),
			SortOrder: &playlistOrder,
		}},
		CanonicalPlaylist: &canonical,
	}
	var err error
	group.GroupKey, err = multidisc.GroupKey(directory, playlist.SHA256)
	if err != nil {
		return PreparedGroup{}, nil, ErrInvalid
	}
	missing := make([]corevalidation.MultiDiscMissingEntry, 0)
	dispositions := []PreparedDisposition{{File: playlist, Disposition: "SOURCE"}}
	for _, entry := range parsed.Entries {
		preparedEntry := PreparedMultiDiscEntry{
			Ordinal: entry.Ordinal, State: string(entry.State), SourceReference: entry.SourceReference,
			NormalizedReference: entry.NormalizedReference, CanonicalName: entry.CanonicalName,
		}
		if entry.State == multidisc.EntryPresent {
			discOrder := entry.Ordinal
			preparedEntry.UploadFileID = entry.File.UploadFileID
			preparedEntry.FileRecord = entry.File.FileRecord
			preparedEntry.SourceLogicalName = entry.File.LogicalName
			sourceFile := ImportFile{
				ID: entry.File.UploadFileID, Path: path.Join(directory, entry.File.Basename),
				FileRecord: entry.File.FileRecord, SHA256: entry.File.BlobSHA256, Size: entry.File.SizeBytes,
			}
			group.Sources = append(group.Sources, PreparedSource{
				File: sourceFile, Role: "DISC", LogicalName: entry.File.LogicalName, SortOrder: &discOrder,
			})
			dispositions = append(dispositions, PreparedDisposition{File: sourceFile, Disposition: "SOURCE"})
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

func (service *ImportPreparation) prepareMultiDiscDirectory(
	directory string,
	files []ImportFile,
	playlist ImportFile,
	limits contentcapability.MultiDiscLimits,
) (PreparedGroup, []PreparedDisposition, error) {
	playlistBytes, err := service.readMultiDiscBlob(playlist, multidisc.MaxPlaylistBytes)
	if err != nil {
		return PreparedGroup{}, nil, err
	}
	candidates, err := service.multiDiscCandidates(files, playlist.ID)
	if err != nil {
		return PreparedGroup{}, nil, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: limits.MaxDiscs, MaxTotalBytes: limits.MaxTotalBytes,
	})
	if err != nil {
		rejection := multiDiscRejection(err, playlist.Path)
		return PreparedGroup{}, []PreparedDisposition{{
			File: playlist, Disposition: "REJECTED", Reason: rejection.Code, Rejection: rejection,
		}}, nil
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return PreparedGroup{}, nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	return preparedMultiDiscGroup(directory, playlist, parsed, canonical)
}

func (service *ImportPreparation) PrepareMultiDiscFiles(
	files []ImportFile,
	limits contentcapability.MultiDiscLimits,
) ([]PreparedDisposition, []PreparedGroup, error) {
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
	groups := make([]PreparedGroup, 0, len(directories))
	for _, directory := range directories {
		playlists := playlistsByDirectory[directory]
		if len(playlists) > 1 {
			for _, playlist := range playlists {
				dispositionByID[playlist.ID] = PreparedDisposition{
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
	dispositions := make([]PreparedDisposition, 0, len(files))
	for _, file := range files {
		dispositions = append(dispositions, dispositionByID[file.ID])
	}
	return dispositions, groups, nil
}
