package libraryimport

import (
	"context"
	"fmt"
	"slices"
)

func (service *Reconfigurations) prepareCloneFiles(ctx context.Context, clone *ReconfigurationClone) error {
	if service.copyFile == nil {
		return ErrInvalid
	}
	clone.Files = slices.Clone(clone.Files)
	for index := range clone.Files {
		file := &clone.Files[index]
		metadata, err := service.copyFile(ctx, file.FileRecord, "staging/uploads/"+clone.UploadID, file.Path)
		if err != nil {
			return fmt.Errorf("copy replacement upload input: %w", err)
		}
		if metadata.Record == "" || metadata.Record == file.FileRecord || metadata.Size != file.Size {
			return ErrVersionConflict
		}
		file.FileRecord = metadata.Record
		clone.Metadata = append(clone.Metadata, metadata)
	}
	return nil
}
