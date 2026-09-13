package libraryimport

import (
	"bytes"
	"context"
	"errors"
	"io"

	"retrom/internal/capability/content/contentmanifest"
	"retrom/internal/capability/content/multidisc"
)

func (service *Service) validateMultiDiscAttachmentContents(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	if err := validateMultiDiscAttachmentSet(candidate.expectedMissing, candidate.uploadFiles); err != nil {
		return err
	}
	playlist, files, err := service.multiDiscAttachmentValidationFiles(ctx, candidate)
	if err != nil {
		return err
	}
	playlistBytes, err := service.readMultiDiscAttachmentPlaylist(playlist)
	if err != nil {
		return err
	}
	parsed, err := multidisc.Parse(playlistBytes, files, multidisc.Limits{
		MaxDiscs: candidate.input.MaxDiscs, MaxTotalBytes: candidate.input.MaxTotalBytes,
	})
	if err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, err)
	}
	if !multiDiscEntriesPresent(parsed.Entries) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return multiDiscAttachmentStoreError("write canonical playlist", err)
	}
	candidate.canonicalPlaylist = canonical
	candidate.resultEntries = parsed.Entries
	return service.buildMultiDiscAttachmentManifest(candidate, playlist)
}

func (service *Service) multiDiscAttachmentValidationFiles(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) (attachedMultiDiscFile, []multidisc.File, error) {
	var playlist attachedMultiDiscFile
	files := make([]multidisc.File, 0, len(candidate.baseEntries))
	for _, file := range candidate.baseFiles {
		if file.role == "PLAYLIST_SOURCE" {
			if playlist.blobID != "" {
				return attachedMultiDiscFile{}, nil,
					multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
			}
			playlist = file
			continue
		}
		validated, err := service.multiDiscFileForValidation(ctx, candidate, file)
		if err != nil {
			return attachedMultiDiscFile{}, nil, err
		}
		files = append(files, validated)
	}
	for _, file := range candidate.uploadFiles {
		validated, err := service.multiDiscFileForValidation(ctx, candidate, file)
		if err != nil {
			return attachedMultiDiscFile{}, nil, err
		}
		files = append(files, validated)
	}
	if playlist.blobID == "" || playlist.blobSize > multidisc.MaxPlaylistBytes {
		return attachedMultiDiscFile{}, nil,
			multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return playlist, files, nil
}

func (service *Service) readMultiDiscAttachmentPlaylist(playlist attachedMultiDiscFile) ([]byte, error) {
	reader, err := service.blobs.OpenDigest(playlist.blobSHA)
	if err != nil {
		return nil, multiDiscAttachmentStoreError("open playlist", err)
	}
	playlistBytes, readErr := io.ReadAll(io.LimitReader(reader, multidisc.MaxPlaylistBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(playlistBytes)) != playlist.blobSize {
		return nil, multiDiscAttachmentStoreError("read playlist", errors.Join(readErr, closeErr))
	}
	return playlistBytes, nil
}

func multiDiscEntriesPresent(entries []multidisc.Entry) bool {
	for _, entry := range entries {
		if entry.State != multidisc.EntryPresent {
			return false
		}
	}
	return true
}

func (service *Service) buildMultiDiscAttachmentManifest(
	candidate *multiDiscAttachmentCandidate,
	playlist attachedMultiDiscFile,
) error {
	files := make([]attachedMultiDiscFile, 0, len(candidate.resultEntries)+1)
	playlist.role, playlist.sortOrder = "PLAYLIST_SOURCE", 0
	files = append(files, playlist)
	manifestFiles := make([]contentmanifest.File, 0, len(candidate.resultEntries)+1)
	manifestFiles = append(manifestFiles, contentmanifest.File{
		Role: playlist.role, LogicalName: playlist.logicalName,
		BlobSHA256: playlist.blobSHA, SizeBytes: playlist.blobSize,
	})
	for _, entry := range candidate.resultEntries {
		file := attachedMultiDiscFile{
			role: "DISC", logicalName: entry.File.LogicalName, uploadFileID: entry.File.UploadFileID,
			blobID: entry.File.BlobID, blobSHA: entry.File.BlobSHA256,
			blobSize: entry.File.SizeBytes, sortOrder: entry.Ordinal,
		}
		files = append(files, file)
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.blobSHA, SizeBytes: file.blobSize,
		})
	}
	manifest, digest, err := contentmanifest.Build(multidisc.ContentKind, manifestFiles)
	if err != nil {
		return multiDiscAttachmentStoreError("build manifest", err)
	}
	candidate.baseFiles = files
	candidate.resultManifestJSON, candidate.resultManifestDigest = string(manifest), digest
	return nil
}
