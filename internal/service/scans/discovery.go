package scans

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"syscall"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
)

type contentReference struct {
	LogicalKey   string `json:"logicalKey"`
	SourcePath   string `json:"sourcePath"`
	RelativeTo   string `json:"relativeTo"`
	RelativePath string `json:"relativePath"`
	Optional     bool   `json:"optional"`
}

func (s *Service) discoverContent(ctx context.Context, root *os.Root, id string, content *sourceContent,
	origins map[string]string, directory model.Directory, inventory *sourceInventory,
) error {
	files := content.Files
	locators := make(map[string]runtimeclient.Locator, len(files))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		absolute, err := s.Storage.Absolute(file.StorageKey)
		if err != nil {
			return wrap(err)
		}
		locators[file.LogicalKey] = runtimeclient.Locator{Path: absolute, Offset: file.Offset, SizeBytes: file.SizeBytes}
		seen[file.LogicalKey] = true
	}
	output, err := s.contentReferences(ctx, root, files, locators, directory, inventory)
	if err != nil {
		return err
	}
	if len(output.Files) > maximumGameFiles {
		return model.ErrInvalid
	}
	for _, reference := range output.Files {
		if !validContentReference(reference, locators) {
			return model.ErrInvalid
		}
		if seen[reference.LogicalKey] {
			continue
		}
		source, exists := origins[reference.RelativeTo]
		if !exists && reference.SourcePath == "" || len(files) >= maximumGameFiles {
			return model.ErrInvalid
		}
		file, err := s.copyReference(ctx, root, id, reference, source)
		if reference.Optional && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if directory.PlatformID == "kirikiri" {
			input := file
			input.LogicalKey = referenceSource(reference, source)
			content.Inputs = append(content.Inputs, input)
		}
		files = append(files, file)
		seen[reference.LogicalKey] = true
	}
	content.Files = files
	return nil
}

func validContentReference(reference contentReference, locators map[string]runtimeclient.Locator) bool {
	if reference.SourcePath != "" {
		return storage.SafeRelative(reference.SourcePath) && storage.SafeRelative(reference.LogicalKey) &&
			reference.RelativeTo == "" && reference.RelativePath == ""
	}
	_, exists := locators[reference.RelativeTo]
	return exists && storage.SafeRelative(reference.LogicalKey) && storage.SafeRelative(reference.RelativeTo) &&
		storage.SafeRelative(reference.RelativePath) &&
		reference.LogicalKey == path.Join(path.Dir(reference.RelativeTo), reference.RelativePath)
}

func (s *Service) copyReference(ctx context.Context, root *os.Root, id string, reference contentReference,
	source string,
) (model.ContentFile, error) {
	name := referenceSource(reference, source)
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return model.ContentFile{}, fmt.Errorf("open referenced source content: %w", err)
	}
	defer closeFile(file)
	info, err := file.Stat()
	if err != nil {
		return model.ContentFile{}, fmt.Errorf("stat referenced source content: %w", err)
	}
	if !info.Mode().IsRegular() {
		return model.ContentFile{}, model.ErrInvalid
	}
	return s.copy(ctx, id, reference.LogicalKey, file)
}

type discoveredContent struct {
	Files        []contentReference `json:"files"`
	SourceSearch *sourceSearch      `json:"sourceSearch"`
}

func (s *Service) contentReferences(ctx context.Context, root *os.Root, files []model.ContentFile,
	locators map[string]runtimeclient.Locator, directory model.Directory, inventory *sourceInventory,
) (*discoveredContent, error) {
	output := &discoveredContent{}
	input := map[string]any{
		"files": runtimeclient.Files(files), "locators": locators,
		"directory":     map[string]any{"platformId": directory.PlatformID, "defaultCoreId": directory.DefaultCoreID},
		"providerRoots": s.Runtime.ProviderRoots(),
	}
	if err := s.Runtime.Call(ctx, "discover-content", input, &output); err != nil {
		return nil, wrap(err)
	}
	if output.SourceSearch != nil && inventory != nil {
		candidates, err := inventory.search(ctx, root, *output.SourceSearch)
		if err != nil {
			return nil, err
		}
		input["sourceCandidates"] = candidates
		if err = s.Runtime.Call(ctx, "discover-content", input, &output); err != nil {
			return nil, wrap(err)
		}
	}
	return output, nil
}

func referenceSource(reference contentReference, source string) string {
	if reference.SourcePath != "" {
		return reference.SourcePath
	}
	return path.Join(path.Dir(source), reference.RelativePath)
}
