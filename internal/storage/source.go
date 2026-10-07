package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"

	"retrom/internal/model"
)

type Sources struct{ Roots []model.Root }

func (s Sources) Open(rootID, relative string) (*os.Root, error) {
	if relative != "" && relative != "." && !SafeRelative(relative) {
		return nil, model.ErrInvalid
	}
	for _, root := range s.Roots {
		if root.ID != rootID {
			continue
		}
		handle, err := os.OpenRoot(root.Path)
		if err != nil {
			return nil, fmt.Errorf("open source root: %w", err)
		}
		if relative == "" || relative == "." {
			return handle, nil
		}
		selected, err := handle.OpenRoot(relative)
		closeErr := handle.Close()
		if err != nil {
			return nil, fmt.Errorf("open source directory: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close source root: %w", closeErr)
		}
		return selected, nil
	}
	return nil, model.ErrNotFound
}

func (s Sources) Directories(rootID, relative string) ([]model.SourceDirectory, error) {
	root, err := s.Open(rootID, relative)
	if err != nil {
		return nil, err
	}
	defer closeRoot(root)
	file, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("read source directory: %w", err)
	}
	defer closeFile(file)
	entries, err := file.ReadDir(1001)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("list source directories: %w", err)
	}
	if len(entries) > 1000 {
		return nil, model.ErrInvalid
	}
	result := make([]model.SourceDirectory, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, model.SourceDirectory{
				RelativePath: path.Join(relative, entry.Name()), Name: entry.Name(),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
