package scans

import (
	"context"
	"fmt"
	"log/slog"
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
	files []model.ContentFile,
) ([]model.ContentFile, error) {
	key, absolute, err := s.Storage.TemporaryPath()
	if err != nil {
		return nil, wrap(err)
	}
	defer func() {
		if removeErr := s.Storage.RemoveTemporary(key); removeErr != nil {
			logProgress(id, removeErr)
		}
	}()
	locators := make(map[string]runtimeclient.Locator, len(files))
	original := make(map[string]model.ContentFile, len(files))
	for _, file := range files {
		locator, locateErr := s.Storage.Absolute(file.StorageKey)
		if locateErr != nil {
			return nil, wrap(locateErr)
		}
		locators[file.LogicalKey] = runtimeclient.Locator{Path: locator, Offset: file.Offset, SizeBytes: file.SizeBytes}
		original[file.LogicalKey] = file
	}
	var output struct {
		Files         []normalizedFile      `json:"files"`
		Normalization *normalizationSummary `json:"normalization"`
	}
	if err = s.Runtime.Call(ctx, "normalize-content", map[string]any{
		"platformId": platform,
		"files":      runtimeclient.Files(files), "locators": locators, "outputRoot": absolute,
	}, &output); err != nil {
		return nil, wrap(err)
	}
	if err = validateNormalizedFiles(output.Files); err != nil {
		return nil, err
	}
	if err = validateNormalizationSummary(output.Normalization); err != nil {
		return nil, err
	}
	result := make([]model.ContentFile, 0, len(output.Files))
	for _, file := range output.Files {
		value, importErr := s.normalizedFile(ctx, id, key, absolute, file, original)
		if importErr != nil {
			return nil, importErr
		}
		result = append(result, value)
	}
	if output.Normalization != nil {
		slog.InfoContext(ctx, "content normalized", "operationId", id, "normalization", output.Normalization)
	}
	return result, nil
}

func (s *Service) normalizedFile(ctx context.Context, id, key, absolute string, file normalizedFile,
	original map[string]model.ContentFile,
) (model.ContentFile, error) {
	if file.Path == "" {
		previous, exists := original[file.LogicalKey]
		if !exists || previous.SHA256 != file.SHA256 || previous.SizeBytes != file.SizeBytes {
			return model.ContentFile{}, model.ErrInvalid
		}
		return previous, nil
	}
	if file.Path != filepath.Join(absolute, filepath.FromSlash(file.LogicalKey)) {
		return model.ContentFile{}, model.ErrInvalid
	}
	source, err := s.Storage.Read(key + "/" + file.LogicalKey)
	if err != nil {
		return model.ContentFile{}, wrap(err)
	}
	defer closeFile(source)
	prepared, err := s.Storage.Stage(ctx, id, source)
	if err != nil {
		return model.ContentFile{}, wrap(err)
	}
	if prepared.SHA256 != file.SHA256 || prepared.Size != file.SizeBytes {
		return model.ContentFile{}, fmt.Errorf("normalized file changed: %w", model.ErrInvalid)
	}
	return model.ContentFile{
		ID: uuid.NewString(), LogicalKey: file.LogicalKey, Role: "rom",
		SHA256: prepared.SHA256, SizeBytes: prepared.Size, StorageKey: prepared.Key,
	}, nil
}

func validateNormalizedFiles(files []normalizedFile) error {
	if len(files) == 0 || len(files) > maximumGameFiles {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(files))
	var total int64
	for _, file := range files {
		if !storage.SafeRelative(file.LogicalKey) || seen[file.LogicalKey] || !model.Hash(file.SHA256) ||
			file.SizeBytes < 0 || file.SizeBytes > storage.MaximumFileSize {
			return model.ErrInvalid
		}
		total += file.SizeBytes
		if total > maximumContentSize {
			return model.ErrInvalid
		}
		seen[file.LogicalKey] = true
	}
	return nil
}
