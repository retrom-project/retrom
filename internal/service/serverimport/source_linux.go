//go:build linux

package serverimport

import (
	"fmt"
	"os"

	"retrom/internal/serversource"
)

func openDirectoryNoFollow(path string) (*os.File, error) {
	directory, err := serversource.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("serverimport/open root: %w", err)
	}
	return directory, nil
}

func duplicateDirectory(parent *os.File) (*os.File, error) {
	directory, err := serversource.DuplicateDirectory(parent)
	if err != nil {
		return nil, fmt.Errorf("serverimport/duplicate directory: %w", err)
	}
	return directory, nil
}
