//go:build !linux

package serverimport

import (
	"os"

	"retrom/internal/serversource"
)

func openDirectoryNoFollow(path string) (*os.File, error) { return serversource.OpenRoot(path) }

func duplicateDirectory(parent *os.File) (*os.File, error) {
	return serversource.DuplicateDirectory(parent)
}
