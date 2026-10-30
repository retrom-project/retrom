package scans

import (
	"context"
	"encoding/json"
	"log/slog"
	"path"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func (s *Service) finalizeSourceContent(ctx context.Context, operation, gameID string,
	directory model.Directory, config json.RawMessage, files []model.ContentFile, source sourceContent,
) ([]model.GameFile, error) {
	physical, err := s.finalizeContent(ctx, gameID, directory, config, files)
	if err != nil {
		return nil, err
	}
	if !source.requiresOrigin(directory, files) || len(physical) != 1 || physical[0].Role != "project_package" {
		return physical, nil
	}
	origin, err := s.preserveOrigin(ctx, operation, gameID, physical[0].ID, source)
	if err != nil {
		s.removePrepared(gameID, physical)
		return nil, wrap(err)
	}
	physical[0].ExtInfo.ConversionInfo = &model.GameFileConversionInfo{OriginInfo: origin}
	return physical, nil
}

func (s *Service) preserveOrigin(ctx context.Context, operation, gameID, fileID string,
	source sourceContent,
) (*model.OriginGameInfo, error) {
	if len(source.Inputs) == 0 {
		return nil, model.ErrInvalid
	}
	var staged storage.File
	logical := "original.zip"
	if source.Bundle {
		var err error
		staged, err = s.Storage.BundleOrigin(ctx, operation, source.Inputs)
		if err != nil {
			return nil, wrap(err)
		}
	} else {
		if len(source.Inputs) != 1 {
			return nil, model.ErrInvalid
		}
		input := source.Inputs[0]
		staged = storage.File{Key: input.StorageKey, Size: input.SizeBytes, SHA256: input.SHA256}
		logical = path.Base(input.LogicalKey)
	}
	info, err := s.Storage.PublishOrigin(ctx, gameID, fileID, logical, staged)
	return info, wrap(err)
}

func (s *Service) finalizeContent(ctx context.Context, id string, directory model.Directory, config json.RawMessage,
	files []model.ContentFile,
) ([]model.GameFile, error) {
	layout, err := s.Runtime.StorageLayout(ctx, directory, config)
	if err != nil {
		return nil, wrap(err)
	}
	if layout == "ZIP_STORE" {
		file, packErr := s.Storage.BuildPackage(ctx, id, files)
		if packErr != nil {
			return nil, wrap(packErr)
		}
		return []model.GameFile{file}, nil
	}
	result := make([]model.GameFile, 0, len(files))
	for _, source := range files {
		file, publishErr := s.Storage.PublishStaged(id, source)
		if publishErr != nil {
			s.removePrepared(id, result)
			return nil, wrap(publishErr)
		}
		result = append(result, file)
	}
	return result, nil
}

func (s *Service) removeStaging(id string) {
	if err := s.Storage.RemoveTemporary("temporary/" + id); err != nil {
		logProgress(id, err)
	}
}

func (s *Service) removePrepared(gameID string, files []model.GameFile) {
	for _, file := range files {
		if err := s.Storage.Remove(file.StorageKey); err != nil {
			slog.Error("remove uncommitted content", "error", err)
		}
		if file.ExtInfo.ConversionInfo != nil && file.ExtInfo.ConversionInfo.OriginInfo != nil {
			if err := s.Storage.RemoveOrigin(gameID, file.ID); err != nil {
				slog.Error("remove uncommitted original", "error", err)
			}
		}
	}
}

// Retention is scoped to KRKR content rewriting. Layout-only packing does not
// justify another copy, and no storage IDs or converter diagnostics select it.
func (source sourceContent) requiresOrigin(directory model.Directory, normalized []model.ContentFile) bool {
	if directory.PlatformID != "kirikiri" {
		return false
	}
	if len(source.Files) != len(normalized) {
		return true
	}
	original := make(map[string]model.ContentFile, len(source.Files))
	for _, file := range source.Files {
		original[file.LogicalKey] = file
	}
	for _, file := range normalized {
		previous, exists := original[file.LogicalKey]
		if !exists || previous.SizeBytes != file.SizeBytes || previous.SHA256 != file.SHA256 {
			return true
		}
	}
	return false
}
