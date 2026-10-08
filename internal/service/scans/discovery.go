package scans

import (
	"context"
	"fmt"
	"os"
	"path"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
)

type contentReference struct {
	LogicalKey   string `json:"logicalKey"`
	RelativeTo   string `json:"relativeTo"`
	RelativePath string `json:"relativePath"`
}

func (s *Service) discoverContent(ctx context.Context, root *os.Root, id string, files []model.GameFile,
	origins map[string]string,
) ([]model.GameFile, error) {
	locators := make(map[string]string, len(files))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		absolute, err := s.Storage.Absolute(file.StorageKey)
		if err != nil {
			return nil, wrap(err)
		}
		locators[file.LogicalKey] = absolute
		seen[file.LogicalKey] = true
	}
	var output struct {
		Files []contentReference `json:"files"`
	}
	if err := s.Runtime.Call(ctx, "discover-content", map[string]any{
		"files": runtimeclient.Files(files), "locators": locators,
	}, &output); err != nil {
		return nil, wrap(err)
	}
	if len(output.Files) > 10000 {
		return nil, model.ErrInvalid
	}
	for _, reference := range output.Files {
		if !validContentReference(reference, locators) {
			return nil, model.ErrInvalid
		}
		if seen[reference.LogicalKey] {
			continue
		}
		source, exists := origins[reference.RelativeTo]
		if !exists || len(files) >= 10000 {
			return nil, model.ErrInvalid
		}
		file, err := s.copyReference(ctx, root, id, reference, source)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
		seen[reference.LogicalKey] = true
	}
	return files, nil
}

func validContentReference(reference contentReference, locators map[string]string) bool {
	_, exists := locators[reference.RelativeTo]
	return exists && storage.SafeRelative(reference.LogicalKey) && storage.SafeRelative(reference.RelativeTo) &&
		storage.SafeRelative(reference.RelativePath) &&
		reference.LogicalKey == path.Join(path.Dir(reference.RelativeTo), reference.RelativePath)
}

func (s *Service) copyReference(ctx context.Context, root *os.Root, id string, reference contentReference,
	source string,
) (model.GameFile, error) {
	file, err := root.Open(path.Join(path.Dir(source), reference.RelativePath))
	if err != nil {
		return model.GameFile{}, fmt.Errorf("open referenced source content: %w", err)
	}
	defer closeFile(file)
	info, err := file.Stat()
	if err != nil {
		return model.GameFile{}, fmt.Errorf("stat referenced source content: %w", err)
	}
	if !info.Mode().IsRegular() {
		return model.GameFile{}, model.ErrInvalid
	}
	return s.copy(ctx, id, reference.LogicalKey, file)
}
