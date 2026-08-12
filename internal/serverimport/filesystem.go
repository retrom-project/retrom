package serverimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"retrom/internal/serversource"
)

var (
	ErrRootIDInvalid   = serversource.ErrRootIDInvalid
	ErrPathInvalid     = serversource.ErrPathInvalid
	ErrRootNotFound    = serversource.ErrRootNotFound
	ErrRootUnavailable = serversource.ErrRootUnavailable
)

type (
	Directory      = serversource.Directory
	discoveredFile = serversource.File
	walkCounts     = serversource.Counts
)

func ValidateRootID(value string) error {
	if err := serversource.ValidateRootID(value); err != nil {
		return fmt.Errorf("serverimport/validate root: %w", err)
	}
	return nil
}

func ValidateRelativePath(value string) error {
	if err := serversource.ValidateRelativePath(value); err != nil {
		return fmt.Errorf("serverimport/validate path: %w", err)
	}
	return nil
}

func openSelectedDirectory(rootPath, relativePath string) (*os.File, error) {
	directory, err := serversource.OpenSelectedDirectory(rootPath, relativePath)
	if err != nil {
		return nil, fmt.Errorf("serverimport/open directory: %w", err)
	}
	return directory, nil
}

func listDirectories(rootPath, relativePath string) ([]Directory, error) {
	directories, err := serversource.ListDirectories(rootPath, relativePath)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list directories: %w", err)
	}
	return directories, nil
}

func walkFiles(root *os.File, limits scanLimits, visit func(discoveredFile) error) (walkCounts, error) {
	counts, err := serversource.WalkFiles(root, serversource.Limits{
		MaxDepth:       limits.maxDepth,
		MaxDirectories: limits.maxDirectories,
		MaxFiles:       limits.maxFiles,
	}, visit)
	if errors.Is(err, serversource.ErrScanLimit) {
		err = ErrScanLimit
	}
	return counts, err
}

func openCandidate(file discoveredFile) (*os.File, fs.FileInfo, error) {
	handle, info, err := serversource.OpenFile(file)
	if errors.Is(err, serversource.ErrSourceChanged) {
		err = errSourceChanged
	}
	return handle, info, err
}

func openRelativeCandidate(rootPath, selectedPath, candidatePath string) (*os.File, fs.FileInfo, error) {
	handle, info, err := serversource.OpenRelativeFile(rootPath, selectedPath, candidatePath)
	if errors.Is(err, serversource.ErrSourceChanged) {
		err = errSourceChanged
	}
	return handle, info, err
}

func sameFileFacts(before, after fs.FileInfo) bool { return serversource.SameFileFacts(before, after) }
