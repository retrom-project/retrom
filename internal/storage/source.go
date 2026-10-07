package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"retrom/internal/model"
)

type Sources struct{}

func (Sources) Open(absolute string) (*os.Root, error) {
	if !filepath.IsAbs(absolute) {
		return nil, model.ErrInvalid
	}
	root, err := os.OpenRoot(filepath.Clean(absolute))
	if err != nil {
		return nil, fmt.Errorf("open source directory: %w", err)
	}
	return root, nil
}

// OpenContent selects a file or project directory before retaining the existing
// relative metadata and content-reading boundary inside its parent directory.
func (s Sources) OpenContent(absolute string) (*os.Root, string, error) {
	if !filepath.IsAbs(absolute) {
		return nil, "", model.ErrInvalid
	}
	selected, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, "", fmt.Errorf("resolve source content: %w", err)
	}
	root, err := s.Open(filepath.Dir(selected))
	return root, filepath.Base(selected), err
}

func (Sources) Directories(absolute string) ([]model.SourceDirectory, error) {
	if !filepath.IsAbs(absolute) {
		return nil, model.ErrInvalid
	}
	absolute = filepath.Clean(absolute)
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return nil, fmt.Errorf("list source directories: %w", err)
	}
	result := make([]model.SourceDirectory, 0)
	for _, entry := range entries {
		name := filepath.Join(absolute, entry.Name())
		directory, statErr := sourceDirectory(entry, name)
		if statErr != nil {
			return nil, statErr
		}
		if directory {
			result = append(result, model.SourceDirectory{Path: name, Name: entry.Name()})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func sourceDirectory(entry os.DirEntry, name string) (bool, error) {
	if entry.Type()&os.ModeSymlink == 0 {
		return entry.IsDir(), nil
	}
	info, err := os.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read source directory link: %w", err)
	}
	return info.IsDir(), nil
}
