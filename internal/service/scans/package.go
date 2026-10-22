package scans

import (
	"context"
	"encoding/json"
	"log/slog"

	"retrom/internal/model"
)

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
			s.removePrepared(result)
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

func (s *Service) removePrepared(files []model.GameFile) {
	for _, file := range files {
		if err := s.Storage.Remove(file.StorageKey); err != nil {
			slog.Error("remove uncommitted content", "error", err)
		}
	}
}
