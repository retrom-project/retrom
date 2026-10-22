package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"retrom/internal/model"
)

// TouchRunResource renews the idle time of a Run-owned resource below runs/<run-id>.
func (s *Store) TouchRunResource(key string, now time.Time) error {
	parts := strings.Split(key, "/")
	if !SafeRelative(key) || len(parts) != 3 || parts[0] != "runs" || !model.UUID(parts[1]) {
		return model.ErrInvalid
	}
	if err := s.root.Chtimes(key, now, now); err != nil {
		return fmt.Errorf("touch run resource: %w", err)
	}
	return nil
}

func (s *Store) RemoveTemporary(key string) error {
	parts := strings.Split(key, "/")
	if len(parts) != 2 || parts[0] != "temporary" || !model.UUID(parts[1]) {
		return model.ErrInvalid
	}
	if err := s.root.RemoveAll(key); err != nil {
		return fmt.Errorf("remove temporary tree: %w", err)
	}
	return nil
}

// StageTree exposes logical files to tools that need a real tree. Standalone immutable files are
// hard-linked; only package members are copied, after checking the space they need.
func (s *Store) StageTree(ctx context.Context, files []model.ContentFile) (string, string, error) {
	if err := s.stageCapacity(files); err != nil {
		return "", "", err
	}
	key, absolute, err := s.TemporaryPath()
	if err != nil {
		return "", "", err
	}
	if err = s.root.Mkdir(key, 0o700); err != nil {
		return "", "", fmt.Errorf("create temporary tree: %w", err)
	}
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return key, "", fmt.Errorf("stage cancelled: %w", err)
		}
		if !SafeRelative(file.LogicalKey) || !SafeRelative(file.StorageKey) {
			return key, "", model.ErrInvalid
		}
		target := path.Join(key, file.LogicalKey)
		if err = s.root.MkdirAll(path.Dir(target), 0o700); err != nil {
			return key, "", fmt.Errorf("create staged directory: %w", err)
		}
		if err = s.stageLogicalFile(ctx, file, target); err != nil {
			return key, "", err
		}
	}
	return key, absolute, nil
}

func (s *Store) stageCapacity(files []model.ContentFile) error {
	var copied int64
	for _, file := range files {
		if file.Package != nil {
			copied += file.SizeBytes
		}
	}
	if copied == 0 {
		return nil
	}
	free, err := s.FreeBytes()
	if err != nil {
		return err
	}
	if free < copied+1024*1024*1024 {
		return model.ErrStorageFull
	}
	return nil
}

func (s *Store) stageLogicalFile(ctx context.Context, file model.ContentFile, target string) error {
	if file.Package == nil && file.Offset == 0 {
		if err := s.root.Link(file.StorageKey, target); err != nil {
			return fmt.Errorf("stage immutable file: %w", err)
		}
		return nil
	}
	source, err := s.OpenContent(file.StorageKey, file.Offset, file.SizeBytes)
	if err != nil {
		return err
	}
	defer closeFile(source)
	destination, err := s.root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create staged member: %w", err)
	}
	defer closeFile(destination)
	if _, err = io.Copy(destination, &contextReader{ctx: ctx, reader: source}); err != nil {
		return diskFailure("stage member", err)
	}
	return nil
}
