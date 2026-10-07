package storage

import (
	"context"
	"fmt"
	"path"
	"strings"

	"retrom/internal/model"
)

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

func (s *Store) StageTree(ctx context.Context, files []model.GameFile) (string, string, error) {
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
		if err = s.root.Link(file.StorageKey, target); err != nil {
			return key, "", fmt.Errorf("stage immutable file: %w", err)
		}
	}
	return key, absolute, nil
}
