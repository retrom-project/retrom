package filestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/cleanup"
)

// RemovablePath excludes the data root, database, secrets and shared directories.
func RemovablePath(relative string) bool {
	if !safeRelativePath(relative) {
		return false
	}
	parts := strings.Split(relative, "/")
	switch parts[0] {
	case "files":
		return len(parts) >= 3 && validObjectID(parts[2]) && GameDirectory(parts[2]) == strings.Join(parts[:3], "/")
	case "staging":
		return len(parts) >= 3 && (parts[1] == "items" || parts[1] == "sources" ||
			parts[1] == "uploads" || parts[1] == "writes") && validObjectID(parts[2])
	case "saves", "bios", "scrapes", "responses", "previews":
		return len(parts) >= 2 && validObjectID(parts[1])
	default:
		return false
	}
}

func (store *Store) RemovePath(ctx context.Context, relative string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("remove domain path: %w", err)
	}
	if !RemovablePath(relative) {
		return ErrRecordInvalid
	}
	if err := store.checkParents(relative); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return fmt.Errorf("open removal root: %w", err)
	}
	defer func() { cleanup.Error("close removal root", root.Close()) }()
	if err := root.RemoveAll(relative); err != nil {
		return fmt.Errorf("remove domain directory: %w", err)
	}
	return syncDirectory(filepath.Join(store.root, filepath.Dir(relative)))
}
