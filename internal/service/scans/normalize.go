package scans

import (
	"context"
	"fmt"
	"path/filepath"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

type normalizedFile struct {
	runtimeclient.File
	Path string `json:"path"`
}

func (s *Service) normalize(ctx context.Context, id, platform string,
	files []model.GameFile,
) ([]model.GameFile, error) {
	key, absolute, err := s.Storage.TemporaryPath()
	if err != nil {
		return nil, wrap(err)
	}
	defer func() {
		if removeErr := s.Storage.RemoveTemporary(key); removeErr != nil {
			logProgress(id, removeErr)
		}
	}()
	locators := make(map[string]string, len(files))
	original := make(map[string]model.GameFile, len(files))
	for _, file := range files {
		locator, locateErr := s.Storage.Absolute(file.StorageKey)
		if locateErr != nil {
			return nil, wrap(locateErr)
		}
		locators[file.LogicalKey] = locator
		original[file.LogicalKey] = file
	}
	var output struct {
		Files []normalizedFile `json:"files"`
	}
	if err = s.Runtime.Call(ctx, "normalize-content", map[string]any{
		"platformId": platform,
		"files":      runtimeclient.Files(files), "locators": locators, "outputRoot": absolute,
	}, &output); err != nil {
		return nil, wrap(err)
	}
	if len(output.Files) == 0 || len(output.Files) > 10000 {
		return nil, model.ErrInvalid
	}
	result := make([]model.GameFile, 0, len(output.Files))
	seen := make(map[string]bool, len(output.Files))
	var total int64
	for _, file := range output.Files {
		total += file.SizeBytes
		if !storage.SafeRelative(file.LogicalKey) || seen[file.LogicalKey] || !model.Hash(file.SHA256) ||
			file.SizeBytes < 0 || total > storage.MaximumFileSize {
			return nil, model.ErrInvalid
		}
		seen[file.LogicalKey] = true
		value, importErr := s.normalizedFile(ctx, id, key, absolute, file, original)
		if importErr != nil {
			return nil, importErr
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Service) normalizedFile(ctx context.Context, id, key, absolute string, file normalizedFile,
	original map[string]model.GameFile,
) (model.GameFile, error) {
	if file.Path == "" {
		previous, exists := original[file.LogicalKey]
		if !exists || previous.SHA256 != file.SHA256 || previous.SizeBytes != file.SizeBytes {
			return model.GameFile{}, model.ErrInvalid
		}
		return previous, nil
	}
	if file.Path != filepath.Join(absolute, filepath.FromSlash(file.LogicalKey)) {
		return model.GameFile{}, model.ErrInvalid
	}
	source, err := s.Storage.Read(key + "/" + file.LogicalKey)
	if err != nil {
		return model.GameFile{}, wrap(err)
	}
	defer closeFile(source)
	prepared, err := s.Storage.Write(ctx, "games", id, source, storage.MaximumFileSize)
	if err != nil {
		return model.GameFile{}, wrap(err)
	}
	if prepared.SHA256 != file.SHA256 || prepared.Size != file.SizeBytes {
		return model.GameFile{}, fmt.Errorf("normalized file changed: %w", model.ErrInvalid)
	}
	return model.GameFile{
		ID: uuid.NewString(), LogicalKey: file.LogicalKey, Role: "rom",
		SHA256: prepared.SHA256, SizeBytes: prepared.Size, StorageKey: prepared.Key,
	}, nil
}
