package libraryimport

import (
	"context"
	"fmt"
	"path"
	"slices"

	"retrom/internal/cleanup"
	"retrom/internal/filestore"
)

// Each review gets independent bytes before the write transaction.
func (service *ImportPreparation) prepareOwnedFiles(ctx context.Context, plan *PreparedImport) error {
	complete := false
	defer func() {
		if !complete {
			service.removePreparedDirectories(ctx, *plan)
		}
	}()
	for index := range plan.Groups {
		group := &plan.Groups[index]
		var err error
		group.ItemID, err = newImportAdmissionID()
		if err != nil {
			return err
		}
		group.Sources = slices.Clone(group.Sources)
		group.MultiEntries = slices.Clone(group.MultiEntries)
		group.ValidationFiles = slices.Clone(group.ValidationFiles)
		copies := map[string]filestore.Metadata{}
		if err := service.copyReviewSources(ctx, group, plan.Archives, copies); err != nil {
			return err
		}
		if err := service.copyReviewDerived(ctx, group, copies); err != nil {
			return err
		}
	}
	complete = true
	return nil
}

func (service *ImportPreparation) copyReviewSources(
	ctx context.Context,
	group *PreparedGroup,
	archives []PreparedArchive,
	copies map[string]filestore.Metadata,
) error {
	for i := range group.Sources {
		source := &group.Sources[i]
		id, _ := preparedFileInput(*source, archives)
		metadata, err := service.copyOwnedFile(ctx, id, group.ItemID, source.LogicalName, copies)
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
		return source.File.FileRecord, nil
	}
	for _, archive := range archives {
		if archive.FileRecord == source.ArchiveFileRecord {
			metadata, ok := archive.Materialized[*source.ArchiveOrdinal]
			if ok {
				return metadata.Record, &metadata
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
		if copied, ok := copies[group.MultiEntries[i].FileRecord]; ok {
			group.MultiEntries[i].FileRecord = copied.Record
		}
	}
	for i := range group.ValidationFiles {
		file := &group.ValidationFiles[i]
		if file.Role == "BIOS_BUNDLE" {
			continue
		}
		input := file.FileRecord
		if file.Artifact != nil {
			input = file.Artifact.Record
		}
		metadata, err := service.copyOwnedFile(ctx, input, group.ItemID, path.Join("derived", file.LogicalName), copies)
		if err != nil {
			return err
		}
		file.Artifact, file.FileRecord = &metadata, ""
	}
	if group.CanonicalPlaylist != nil {
		copied, err := service.copyOwnedFile(ctx, group.CanonicalPlaylist.Record, group.ItemID,
			"derived/playlist.m3u", copies)
		if err != nil {
			return err
		}
		group.CanonicalPlaylist = &copied
	}
	if group.BundleFileRecord == "" && group.Bundle == nil {
		return nil
	}
	input := group.BundleFileRecord
	if group.Bundle != nil {
		input = group.Bundle.Record
	}
	metadata, err := service.copyOwnedFile(ctx, input, group.ItemID, "derived/bundle", copies)
	if err != nil {
		return err
	}
	group.Bundle, group.BundleFileRecord = &metadata, ""
	return nil
}

func (service *ImportPreparation) copyOwnedFile(
	ctx context.Context,
	id, itemID, name string,
	copies map[string]filestore.Metadata,
) (filestore.Metadata, error) {
	if metadata, ok := copies[id]; ok {
		return metadata, nil
	}
	if service.blobs == nil || id == "" {
		return filestore.Metadata{}, ErrInvalid
	}
	metadata, err := service.blobs.CopyTo(ctx, id, filestore.ItemDirectory(itemID)+"/payload/content/"+itemID, name)
	if err != nil {
		return filestore.Metadata{}, fmt.Errorf("copy review input: %w", err)
	}
	copies[id] = metadata
	return metadata, nil
}

// Uncommitted groups have freshly allocated IDs and cannot belong to another import.
func (service *ImportPreparation) removePreparedDirectories(ctx context.Context, plan PreparedImport) {
	if service == nil || service.blobs == nil {
		return
	}
	for _, group := range plan.Groups {
		if group.ItemID != "" {
			cleanup.Error("remove uncommitted review directory",
				service.blobs.RemovePath(context.WithoutCancel(ctx), filestore.ItemDirectory(group.ItemID)))
		}
	}
}
