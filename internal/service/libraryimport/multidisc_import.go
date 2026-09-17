package libraryimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	model "retrom/internal/model/libraryimport"
	"sort"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	"retrom/internal/capability/format/importing"
	"retrom/internal/foundation/cleanup"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *ImportPreparation) PrepareImportFiles(
	ctx context.Context, platformID, sourceType string, files []model.ImportFile, datID string,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	if platformID == "arcade" {
		return service.PrepareArcadeFiles(ctx, files, datID)
	}
	var dispositions []model.PreparedDisposition
	var groups []model.PreparedGroup
	var archives []model.PreparedArchive
	if platformID == "dos" {
		dispositions, groups, archives = service.PrepareDOSFiles(ctx, sourceType, files)
	} else {
		profile, exists := contentprofile.ByPlatform(platformID)
		if exists {
			dispositions, groups, archives = service.prepareProfileFiles(ctx, platformID, profile, files)
		} else {
			dispositions = prepareUnsupportedPlatformFiles(files)
		}
	}
	return dispositions, groups, archives, nil
}

func prepareUnsupportedPlatformFiles(
	files []model.ImportFile,
) []model.PreparedDisposition {
	dispositions := make([]model.PreparedDisposition, 0, len(files))
	for _, file := range files {
		disposition := model.PreparedDisposition{
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
	profile contentprofile.Profile,
	files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive) {
	dispositions := make([]model.PreparedDisposition, 0, len(files))
	groups := make([]model.PreparedGroup, 0, len(files))
	archives := make([]model.PreparedArchive, 0)
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

func (service *ImportPreparation) prepareProfileFile(
	ctx context.Context,
	platformID string,
	profile contentprofile.Profile,
	file model.ImportFile,
) (model.PreparedDisposition, *model.PreparedGroup, *model.PreparedArchive) {
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
		return rejectedDisposition(file, ArchiveReason(err)), nil, nil
	}
	candidate, err := contentprofile.SelectArchivePrimary(platformID, entries)
	if err != nil {
		return rejectedDisposition(file, archiveSelectionReason(err)), nil, nil
	}
	selected, err := service.materializeArchiveEntry(ctx, service.blobs.Path(file.SHA256), candidate)
	if err != nil {
		return rejectedDisposition(file, ArchiveReason(err)), nil, nil
	}
	ordinal := candidate.Ordinal
	group := &model.PreparedGroup{Sources: []model.PreparedSource{{
		File: file, Role: "CONTENT", LogicalName: filepath.Base(candidate.NormalizedPath),
		ArchiveBlobID: file.BlobID, ArchiveOrdinal: &ordinal,
	}}}
	archive := &model.PreparedArchive{
		BlobID: file.BlobID, Entries: entries,
		Materialized: map[int]blobstore.Metadata{ordinal: selected},
	}
	return sourceDisposition(file), group, archive
}

func ignoredDisposition(file model.ImportFile) model.PreparedDisposition {
	return model.PreparedDisposition{File: file, Disposition: "IGNORED", Reason: "IGNORED_SYSTEM_SIDECAR"}
}

func sourceDisposition(file model.ImportFile) model.PreparedDisposition {
	return model.PreparedDisposition{File: file, Disposition: "SOURCE"}
}

func rejectedDisposition(file model.ImportFile, reason string) model.PreparedDisposition {
	return model.PreparedDisposition{File: file, Disposition: "REJECTED", Reason: reason}
}

func singleSourceGroup(file model.ImportFile, logicalName string) *model.PreparedGroup {
	return &model.PreparedGroup{Sources: []model.PreparedSource{{File: file, Role: "CONTENT", LogicalName: logicalName}}}
}

func profileArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	return ImportArchiveFormat(filePath)
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
		return ArchiveReason(err)
	}
}

func (service *ImportPreparation) scanProfileArchive(
	ctx context.Context,
	file model.ImportFile,
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

func (service *ImportPreparation) readMultiDiscBlob(file model.ImportFile, maximum int64) ([]byte, error) {
	if service.blobs == nil || file.Size > maximum {
		return nil, model.ErrInvalid
	}
	reader, err := service.blobs.OpenDigest(file.SHA256)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil || int64(len(contents)) != file.Size || int64(len(contents)) > maximum {
		return nil, model.ErrInvalid
	}
	return contents, nil
}

func (service *ImportPreparation) readMultiDiscHeader(file model.ImportFile) ([]byte, error) {
	if service.blobs == nil || file.Size < 8 {
		return nil, model.ErrInvalid
	}
	reader, err := service.blobs.OpenDigest(file.SHA256)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	header := make([]byte, 8)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, model.ErrInvalid
	}
	return header, nil
}

func multiDiscFileBuckets(
	files []model.ImportFile,
) (map[string][]model.ImportFile, map[string][]model.ImportFile, int) {
	playlistsByDirectory := make(map[string][]model.ImportFile)
	filesByDirectory := make(map[string][]model.ImportFile)
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

func initialMultiDiscDispositions(files []model.ImportFile) map[string]model.PreparedDisposition {
	dispositionByID := make(map[string]model.PreparedDisposition, len(files))
	for _, file := range files {
		reason := "NOT_REFERENCED_BY_PLAYLIST"
		if knownSidecar(file.Path) {
			reason = "IGNORED_SYSTEM_SIDECAR"
		}
		dispositionByID[file.ID] = model.PreparedDisposition{File: file, Disposition: "IGNORED", Reason: reason}
	}
	return dispositionByID
}

func (service *ImportPreparation) multiDiscCandidates(
	files []model.ImportFile,
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
	playlist model.ImportFile,
	parsed multidisc.Result,
	canonical blobstore.Metadata,
) (model.PreparedGroup, []model.PreparedDisposition, error) {
	playlistOrder := 0
	group := model.PreparedGroup{
		ContentKind: multidisc.ContentKind, TitleSource: path.Base(playlist.Path),
		Sources: []model.PreparedSource{{
			File: playlist, Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.Path),
			SortOrder: &playlistOrder,
		}},
		CanonicalPlaylist: &canonical,
	}
	var err error
	group.GroupKey, err = multidisc.GroupKey(directory, playlist.SHA256)
	if err != nil {
		return model.PreparedGroup{}, nil, model.ErrInvalid
	}
	missing := make([]corevalidation.MultiDiscMissingEntry, 0)
	dispositions := []model.PreparedDisposition{{File: playlist, Disposition: "SOURCE"}}
	for _, entry := range parsed.Entries {
		preparedEntry := model.PreparedMultiDiscEntry{
			Ordinal: entry.Ordinal, State: string(entry.State), SourceReference: entry.SourceReference,
			NormalizedReference: entry.NormalizedReference, CanonicalName: entry.CanonicalName,
		}
		if entry.State == multidisc.EntryPresent {
			discOrder := entry.Ordinal
			preparedEntry.UploadFileID = entry.File.UploadFileID
			preparedEntry.BlobID = entry.File.BlobID
			preparedEntry.SourceLogicalName = entry.File.LogicalName
			sourceFile := model.ImportFile{
				ID: entry.File.UploadFileID, Path: path.Join(directory, entry.File.Basename),
				BlobID: entry.File.BlobID, SHA256: entry.File.BlobSHA256, Size: entry.File.SizeBytes,
			}
			group.Sources = append(group.Sources, model.PreparedSource{
				File: sourceFile, Role: "DISC", LogicalName: entry.File.LogicalName, SortOrder: &discOrder,
			})
			dispositions = append(dispositions, model.PreparedDisposition{File: sourceFile, Disposition: "SOURCE"})
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
	files []model.ImportFile,
	playlist model.ImportFile,
	limits contentcapability.MultiDiscLimits,
) (model.PreparedGroup, []model.PreparedDisposition, error) {
	playlistBytes, err := service.readMultiDiscBlob(playlist, multidisc.MaxPlaylistBytes)
	if err != nil {
		return model.PreparedGroup{}, nil, err
	}
	candidates, err := service.multiDiscCandidates(files, playlist.ID)
	if err != nil {
		return model.PreparedGroup{}, nil, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: limits.MaxDiscs, MaxTotalBytes: limits.MaxTotalBytes,
	})
	if err != nil {
		return model.PreparedGroup{}, []model.PreparedDisposition{{
			File: playlist, Disposition: "REJECTED", Reason: multiDiscParseReason(err),
		}}, nil
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return model.PreparedGroup{}, nil, fmt.Errorf("libraryimport/multidisc: %w", err)
	}
	return preparedMultiDiscGroup(directory, playlist, parsed, canonical)
}

func (service *ImportPreparation) PrepareMultiDiscFiles(
	files []model.ImportFile,
	limits contentcapability.MultiDiscLimits,
) ([]model.PreparedDisposition, []model.PreparedGroup, error) {
	playlistsByDirectory, filesByDirectory, playlistCount := multiDiscFileBuckets(files)
	if playlistCount == 0 {
		return nil, nil, model.ErrMultiDiscPlaylistMissing
	}
	directories := make([]string, 0, len(playlistsByDirectory))
	for directory := range playlistsByDirectory {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	dispositionByID := initialMultiDiscDispositions(files)
	groups := make([]model.PreparedGroup, 0, len(directories))
	for _, directory := range directories {
		playlists := playlistsByDirectory[directory]
		if len(playlists) > 1 {
			for _, playlist := range playlists {
				dispositionByID[playlist.ID] = model.PreparedDisposition{
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
	dispositions := make([]model.PreparedDisposition, 0, len(files))
	for _, file := range files {
		dispositions = append(dispositions, dispositionByID[file.ID])
	}
	return dispositions, groups, nil
}
