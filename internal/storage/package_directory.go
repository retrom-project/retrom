package storage

import (
	"archive/zip"
	"context"
	"fmt"

	"retrom/internal/model"
)

// ValidatePackageDirectory checks an existing immutable object's complete index without rewriting it.
func (s *Store) ValidatePackageDirectory(ctx context.Context, file model.GameFile, files []model.ContentFile) error {
	physical, err := s.Read(file.StorageKey)
	if err != nil {
		return err
	}
	defer closeFile(physical)
	info, err := physical.Stat()
	if err != nil {
		return fmt.Errorf("stat stored package: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != file.SizeBytes {
		return model.ErrInvalid
	}
	archive, err := zip.NewReader(physical, info.Size())
	if err != nil {
		return fmt.Errorf("read stored package: %w", err)
	}
	if len(archive.File) != len(files) {
		return model.ErrInvalid
	}
	byName := make(map[string]model.ContentFile, len(files))
	for _, member := range files {
		byName[member.LogicalKey] = member
	}
	for _, entry := range archive.File {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("validate package directory: %w", err)
		}
		member, exists := byName[entry.Name]
		if !exists || !matchesStoredEntry(entry, member) {
			return model.ErrInvalid
		}
		delete(byName, entry.Name)
	}
	if len(byName) != 0 {
		return model.ErrInvalid
	}
	return nil
}

func matchesStoredEntry(entry *zip.File, member model.ContentFile) bool {
	offset, err := entry.DataOffset()
	return err == nil && entry.Method == zip.Store && entry.Flags&1 == 0 && offset == member.Offset &&
		entry.UncompressedSize64 == uint64(member.SizeBytes)
}
