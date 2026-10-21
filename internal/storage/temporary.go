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

func (s *Store) TouchTemporary(key string, now time.Time) error {
	if !SafeRelative(key) || !strings.HasPrefix(key, "temporary/") {
		return model.ErrInvalid
	}
	if err := s.root.Chtimes(key, now, now); err != nil {
		return fmt.Errorf("touch temporary resource: %w", err)
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

func (s *Store) StageTree(ctx context.Context, files []model.ContentFile) (string, string, error) {
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

func (s *Store) stageLogicalFile(ctx context.Context, file model.ContentFile, target string) error {
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
		return fmt.Errorf("stage member: %w", err)
	}
	return nil
}
