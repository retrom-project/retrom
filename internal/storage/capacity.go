package storage

import (
	"errors"
	"fmt"
	"syscall"

	"retrom/internal/model"
)

func (s *Store) FreeBytes() (int64, error) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(s.directory, &status); err != nil {
		return 0, fmt.Errorf("managed free space: %w", err)
	}
	return int64(status.Bavail) * status.Bsize, nil
}

func diskFailure(action string, err error) error {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		err = errors.Join(model.ErrStorageFull, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}
