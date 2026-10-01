package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
)

func (service *Service) prepareMultiDiscDirectory(ctx context.Context, candidate *multiDiscAttachmentCandidate) error {
	directory := filestore.ItemDirectory(candidate.input.ImportItemID) + "/payload/content/" + candidate.input.AttachmentID
	copies := map[string]string{}
	for i := range candidate.baseFiles {
		file := &candidate.baseFiles[i]
		copied, err := service.blobs.CopyTo(ctx, file.fileRecord, directory+"/source", file.logicalName)
		if err != nil {
			return fmt.Errorf("prepare multi disc directory: %w", err)
		}
		copies[file.fileRecord] = copied.Record
		file.fileRecord = copied.Record
	}
	for i := range candidate.resultEntries {
		file := candidate.resultEntries[i].File
		if copied, found := copies[file.FileRecord]; found {
			file.FileRecord = copied
		}
	}
	copied, err := service.blobs.CopyTo(ctx, candidate.canonicalPlaylist.Record, directory+"/derived", "playlist.m3u")
	if err != nil {
		return fmt.Errorf("prepare multi disc directory: %w", err)
	}
	candidate.canonicalPlaylist = copied
	return nil
}

func (service *Service) prepareParentDirectory(ctx context.Context,
	candidate *parentAttachmentCandidate, files []attachedSourceFile, validation *preparedGroup,
) (map[string]string, error) {
	directory := filestore.ItemDirectory(candidate.itemID) + "/payload/content/" + candidate.attachmentID
	copies := map[string]string{}
	for i := range files {
		file := &files[i]
		copied, err := service.blobs.CopyTo(ctx, file.fileRecord, directory+"/source", file.logicalName)
		if err != nil {
			return nil, fmt.Errorf("prepare parent directory: %w", err)
		}
		copies[file.fileRecord] = copied.Record
		file.fileRecord = copied.Record
	}
	for i := range validation.ValidationFiles {
		file := &validation.ValidationFiles[i]
		if file.Role == "BIOS_BUNDLE" {
			continue
		}
		if copied, ok := copies[file.FileRecord]; ok {
			file.FileRecord = copied
			continue
		}
		copied, err := service.blobs.CopyTo(ctx, file.FileRecord, directory+"/derived", file.LogicalName)
		if err != nil {
			return nil, fmt.Errorf("prepare parent directory: %w", err)
		}
		file.FileRecord = copied.Record
	}
	if copied, ok := copies[candidate.fileRecord]; ok {
		candidate.fileRecord = copied
	}
	return copies, nil
}
