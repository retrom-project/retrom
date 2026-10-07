package scans

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path"

	"retrom/internal/model"

	"github.com/google/uuid"
)

func (s *Service) media(ctx context.Context, root *os.Root, id string, candidate Candidate) ([]model.Media, error) {
	result := make([]model.Media, 0)
	for _, source := range []struct{ name, kind string }{{candidate.Cover, "cover"}, {candidate.Video, "video"}} {
		if source.name == "" {
			continue
		}
		name, err := declared(".", source.name)
		if err != nil {
			return nil, err
		}
		reader, err := root.Open(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("open source media: %w", err)
		}
		file, copyErr := s.Storage.Write(ctx, "games", id, reader, 512*1024*1024)
		closeErr := reader.Close()
		if copyErr != nil {
			return nil, wrap(copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close source media: %w", closeErr)
		}
		mediaType := mime.TypeByExtension(path.Ext(name))
		if mediaType == "" {
			return nil, model.ErrInvalid
		}
		result = append(result,
			model.Media{
				ID:         uuid.NewString(),
				Kind:       source.kind,
				Ordinal:    0,
				StorageKey: file.Key,
				SHA256:     file.SHA256,
				SizeBytes:  file.Size,
				MediaType:  mediaType,
			})
	}
	return result, nil
}

func closeFile(reader io.Closer) {
	if err := reader.Close(); err != nil {
		slog.Error("close scan source", "error", err)
	}
}

func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		slog.Error("close scan root", "error", err)
	}
}
