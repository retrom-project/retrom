package gamecontent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentmanifest"
	"retrom/internal/contentprofile"
	"retrom/internal/multidisc"
)

func (service *Service) prepareReplacement(
	ctx context.Context,
	snapshot JobSnapshot,
	files []UploadedFile,
) (PreparedReplacement, error) {
	if snapshot.ContentMode == contentcapability.ModeRPGMakerProject {
		return service.prepareRPGMakerReplacement(ctx, snapshot, files)
	}
	if snapshot.ContentMode != contentcapability.ModeMultiDisc {
		if len(files) == 0 || snapshot.PlatformID != "dos" && len(files) != 1 ||
			snapshot.PlatformID == "arcade" && !strings.EqualFold(filepath.Ext(files[0].LogicalName), ".zip") {
			return PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_GROUP_INVALID"}
		}
		replacement := PreparedReplacement{ContentKind: string(contentprofile.ContentKindSingleFile)}
		replacement.Files = make([]ReplacementFile, 0, len(files))
		manifestFiles := make([]contentmanifest.File, 0, len(files))
		for index, file := range files {
			role := "COMPANION"
			if index == 0 {
				role = "CONTENT"
			}
			replacement.Files = append(replacement.Files, ReplacementFile{
				Role: role, LogicalName: file.LogicalName, BlobID: file.BlobID,
				SHA256: file.SHA256, SizeBytes: file.SizeBytes, SortOrder: index,
			})
			manifestFiles = append(manifestFiles, contentmanifest.File{
				Role: role, LogicalName: file.LogicalName, BlobSHA256: file.SHA256, SizeBytes: file.SizeBytes,
			})
		}
		replacement.FirstContentLogicalName = files[0].LogicalName
		manifest, digest, err := contentmanifest.Build(replacement.ContentKind, manifestFiles)
		if err != nil {
			return PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
		}
		replacement.Manifest, replacement.ManifestDigest = manifest, digest
		return replacement, nil
	}
	return service.prepareMultiDiscReplacement(ctx, snapshot, files)
}

func (service *Service) prepareMultiDiscReplacement(
	ctx context.Context,
	snapshot JobSnapshot,
	files []UploadedFile,
) (PreparedReplacement, error) {
	if err := ctx.Err(); err != nil {
		return PreparedReplacement{}, fmt.Errorf("prepare multi-disc replacement: %w", err)
	}
	if service.blobs == nil || snapshot.MaxDiscs < multidisc.MinDiscs || snapshot.MaxDiscs > multidisc.MaxDiscs ||
		snapshot.MaxTotalBytes <= 0 {
		return PreparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_VALIDATION_UNAVAILABLE"}
	}
	playlist, err := replacementPlaylist(files)
	if err != nil {
		return PreparedReplacement{}, err
	}
	playlistBytes, err := service.readReplacementPlaylist(playlist)
	if err != nil {
		return PreparedReplacement{}, err
	}
	directory := path.Dir(playlist.LogicalName)
	candidates, err := service.replacementDiscCandidates(files, playlist.BlobID, directory)
	if err != nil {
		return PreparedReplacement{}, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: snapshot.MaxDiscs, MaxTotalBytes: snapshot.MaxTotalBytes,
	})
	if err != nil {
		var validationErr *multidisc.ValidationError
		if errors.As(err, &validationErr) {
			return PreparedReplacement{}, &replacementValidationError{code: string(validationErr.Code)}
		}
		return PreparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_INVALID"}
	}
	for _, entry := range parsed.Entries {
		if entry.State != multidisc.EntryPresent || entry.File == nil {
			return PreparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_FILE_MISSING"}
		}
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return PreparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_VALIDATION_UNAVAILABLE"}
	}
	return buildPreparedMultiDiscReplacement(playlist, parsed, canonical)
}

func replacementPlaylist(files []UploadedFile) (UploadedFile, error) {
	playlists := make([]UploadedFile, 0, 2)
	for _, file := range files {
		if strings.EqualFold(path.Ext(file.LogicalName), ".m3u") {
			playlists = append(playlists, file)
		}
	}
	if len(playlists) == 0 {
		return UploadedFile{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_MISSING"}
	}
	if len(playlists) != 1 {
		return UploadedFile{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_AMBIGUOUS"}
	}
	return playlists[0], nil
}

func (service *Service) readReplacementPlaylist(playlist UploadedFile) ([]byte, error) {
	playlistFile, err := service.blobs.OpenDigest(playlist.SHA256)
	if err != nil {
		return nil, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	defer func() { cleanup.Error("close", playlistFile.Close()) }()
	playlistBytes, err := io.ReadAll(io.LimitReader(playlistFile, multidisc.MaxPlaylistBytes+1))
	if err != nil {
		return nil, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	return playlistBytes, nil
}

func (service *Service) replacementDiscCandidates(
	files []UploadedFile,
	playlistBlobID, directory string,
) ([]multidisc.File, error) {
	candidates := make([]multidisc.File, 0, len(files))
	for _, file := range files {
		if file.BlobID == playlistBlobID || path.Dir(file.LogicalName) != directory ||
			!strings.EqualFold(path.Ext(file.LogicalName), ".chd") {
			continue
		}
		candidate, err := service.replacementDiscCandidate(file)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (service *Service) replacementDiscCandidate(file UploadedFile) (multidisc.File, error) {
	blob, err := service.blobs.OpenDigest(file.SHA256)
	if err != nil {
		return multidisc.File{}, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	defer func() { cleanup.Error("close", blob.Close()) }()
	header := make([]byte, 8)
	if _, err := io.ReadFull(blob, header); err != nil {
		return multidisc.File{}, &replacementValidationError{code: string(multidisc.CodeCHDInvalid)}
	}
	return multidisc.File{
		Basename: path.Base(file.LogicalName), LogicalName: path.Base(file.LogicalName),
		BlobID: file.BlobID, BlobSHA256: file.SHA256, SizeBytes: file.SizeBytes, Header: header,
	}, nil
}

func buildPreparedMultiDiscReplacement(
	playlist UploadedFile,
	parsed multidisc.Result,
	canonical blobstore.Metadata,
) (PreparedReplacement, error) {
	replacement := PreparedReplacement{
		ContentKind: multidisc.ContentKind, CanonicalPlaylist: canonical,
		Files:                   make([]ReplacementFile, 0, len(parsed.Entries)+1),
		OrderedDiscSHA256:       make([]string, 0, len(parsed.Entries)),
		FirstContentLogicalName: parsed.Entries[0].File.LogicalName,
	}
	replacement.Files = append(replacement.Files, ReplacementFile{
		Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.LogicalName), BlobID: playlist.BlobID,
		SHA256: playlist.SHA256, SizeBytes: playlist.SizeBytes, SortOrder: 0,
	})
	manifestFiles := make([]contentmanifest.File, 0, len(parsed.Entries)+1)
	manifestFiles = append(manifestFiles, contentmanifest.File{
		Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.LogicalName),
		BlobSHA256: playlist.SHA256, SizeBytes: playlist.SizeBytes,
	})
	for _, entry := range parsed.Entries {
		file := ReplacementFile{
			Role: "DISC", LogicalName: entry.File.LogicalName, BlobID: entry.File.BlobID,
			SHA256: entry.File.BlobSHA256, SizeBytes: entry.File.SizeBytes, SortOrder: entry.Ordinal,
		}
		replacement.Files = append(replacement.Files, file)
		replacement.OrderedDiscSHA256 = append(replacement.OrderedDiscSHA256, file.SHA256)
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: file.Role, LogicalName: file.LogicalName, BlobSHA256: file.SHA256, SizeBytes: file.SizeBytes,
		})
	}
	manifest, manifestDigest, err := contentmanifest.Build(replacement.ContentKind, manifestFiles)
	if err != nil {
		return PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	replacement.Manifest, replacement.ManifestDigest = manifest, manifestDigest
	return replacement, nil
}
