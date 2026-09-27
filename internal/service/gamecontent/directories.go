package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
)

func replacementDirectory(snapshot JobSnapshot) string {
	return filestore.GameDirectory(snapshot.GameID) + "/content/" + snapshot.ExecutionID
}

func (service *Service) prepareDirectory(ctx context.Context, snapshot JobSnapshot,
	prepared PreparedReplacement,
) (PreparedReplacement, error) {
	directory := replacementDirectory(snapshot)
	for i := range prepared.Files {
		file := &prepared.Files[i]
		copied, err := service.blobs.CopyTo(ctx, file.FileRecord, directory+"/source", file.LogicalName)
		if err != nil {
			return PreparedReplacement{}, fmt.Errorf("prepare directory: %w", err)
		}
		prepared.FileCopies = append(prepared.FileCopies, FileCopy{Source: file.FileRecord, Target: copied.Record})
		file.FileRecord = copied.Record
	}
	if prepared.CanonicalPlaylist.Record != "" {
		copied, err := service.blobs.CopyTo(ctx, prepared.CanonicalPlaylist.Record, directory+"/derived", "playlist.m3u")
		if err != nil {
			return PreparedReplacement{}, fmt.Errorf("prepare directory: %w", err)
		}
		prepared.CanonicalPlaylist = copied
	}
	if prepared.RPGMaker != nil {
		for i := range prepared.RPGMaker.VariantFiles {
			file := &prepared.RPGMaker.VariantFiles[i]
			copied, err := service.blobs.CopyTo(ctx, file.Metadata.Record, directory+"/derived", file.LogicalName)
			if err != nil {
				return PreparedReplacement{}, fmt.Errorf("prepare directory: %w", err)
			}
			file.Metadata = copied
		}
	}
	return prepared, nil
}
