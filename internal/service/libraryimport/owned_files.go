package libraryimport

import (
	"context"
	"fmt"
	"slices"

	"retrom/internal/filestore"
)

// Each review gets independent bytes before the write transaction.
func (service *ImportPreparation) prepareOwnedFiles(ctx context.Context, plan *PreparedImport) error {
	assigned := map[string]bool{}
	for index := range plan.Groups {
		group := &plan.Groups[index]
		group.Sources = slices.Clone(group.Sources)
		group.MultiEntries = slices.Clone(group.MultiEntries)
		group.ValidationFiles = slices.Clone(group.ValidationFiles)
		copies := map[string]filestore.Metadata{}
		if err := service.copyReviewSources(ctx, group, plan.Archives, assigned, copies); err != nil {
			return err
		}
		if err := service.copyReviewDerived(ctx, group, copies); err != nil {
			return err
		}
	}
	return nil
}

func (service *ImportPreparation) copyReviewSources(
	ctx context.Context,
	group *PreparedGroup,
	archives []PreparedArchive,
	assigned map[string]bool,
	copies map[string]filestore.Metadata,
) error {
	for i := range group.Sources {
		source := &group.Sources[i]
		id, extracted := preparedFileInput(*source, archives)
		if extracted != nil && !assigned[id] {
			copies[id] = *extracted
			assigned[id] = true
		}
		metadata, err := service.copyOwnedFile(ctx, id, copies)
		if err != nil {
			return err
		}
		digest, size, err := preparedSourceIdentity(*source, archives)
		if err != nil {
			return err
		}
		if metadata.SHA256 != digest || metadata.Size != size {
			return ErrVersionConflict
		}
		source.Payload = &metadata
	}
	return nil
}

func preparedFileInput(source PreparedSource, archives []PreparedArchive) (string, *filestore.Metadata) {
	if source.ArchiveOrdinal == nil {
		return source.File.BlobID, nil
	}
	for _, archive := range archives {
		if archive.BlobID == source.ArchiveBlobID {
			metadata, ok := archive.Materialized[*source.ArchiveOrdinal]
			if ok {
				return metadata.ID, &metadata
			}
		}
	}
	return "", nil
}

func (service *ImportPreparation) copyReviewDerived(
	ctx context.Context,
	group *PreparedGroup,
	copies map[string]filestore.Metadata,
) error {
	for i := range group.MultiEntries {
		if copied, ok := copies[group.MultiEntries[i].BlobID]; ok {
			group.MultiEntries[i].BlobID = copied.ID
		}
	}
	for i := range group.ValidationFiles {
		file := &group.ValidationFiles[i]
		if file.Role == "BIOS_BUNDLE" || file.Artifact != nil {
			continue
		}
		metadata, err := service.copyOwnedFile(ctx, file.BlobID, copies)
		if err != nil {
			return err
		}
		file.Artifact, file.BlobID = &metadata, ""
	}
	if group.BundleBlobID == "" {
		return nil
	}
	metadata, err := service.copyOwnedFile(ctx, group.BundleBlobID, copies)
	if err != nil {
		return err
	}
	group.Bundle, group.BundleBlobID = &metadata, ""
	return nil
}

func (service *ImportPreparation) copyOwnedFile(
	ctx context.Context,
	id string,
	copies map[string]filestore.Metadata,
) (filestore.Metadata, error) {
	if metadata, ok := copies[id]; ok {
		return metadata, nil
	}
	if service.blobs == nil || id == "" {
		return filestore.Metadata{}, ErrInvalid
	}
	metadata, err := service.blobs.Copy(ctx, id)
	if err != nil {
		return filestore.Metadata{}, fmt.Errorf("copy review input: %w", err)
	}
	copies[id] = metadata
	return metadata, nil
}
