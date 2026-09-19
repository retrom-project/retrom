package serverimport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	servermodel "retrom/internal/model/serverimport"

	"retrom/internal/adapter/files/serversource"
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

func listDirectories(rootPath, relativePath string) ([]serversource.Directory, error) {
	directories, err := serversource.ListDirectories(rootPath, relativePath)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list directories: %w", err)
	}
	return directories, nil
}

func walkFiles(
	ctx context.Context, root *os.File, limits scanLimits, visit func(serversource.File) error,
) (servermodel.DiscoveryCounts, error) {
	counts, err := serversource.WalkFilesContext(ctx, root, serversource.Limits{
		MaxDepth:       limits.maxDepth,
		MaxDirectories: limits.maxDirectories,
		MaxFiles:       limits.maxFiles,
	}, visit)
	if errors.Is(err, serversource.ErrScanLimit) {
		err = ErrScanLimit
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("%w: %w", errExecutionDeadline, err)
	}
	return counts, err
}

func openCandidate(file serversource.File) (*os.File, fs.FileInfo, error) {
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
