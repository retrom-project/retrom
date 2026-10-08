package runs

import (
	"context"
	"fmt"
	"log/slog"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func (s *Service) assembled(ctx context.Context, run *Context, selected []Blob,
	plan runtimeclient.Resource,
) (Blob, error) {
	locators := make(map[string]string, len(selected))
	facts := make([]runtimeclient.File, 0, len(selected))
	for _, file := range selected {
		absolute, err := s.Storage.Absolute(file.Key)
		if err != nil {
			return Blob{}, wrap(err)
		}
		locators[file.LogicalPath] = absolute
		facts = append(facts, runtimeclient.File{
			LogicalKey: file.LogicalPath, Name: file.LogicalPath,
			SHA256: file.SHA256, SizeBytes: file.SizeBytes,
		})
	}
	key, absolute, err := s.Storage.TemporaryPath()
	if err != nil {
		return Blob{}, wrap(err)
	}
	defer func() {
		if removeErr := s.Storage.Remove(key); removeErr != nil {
			slog.Error("remove resource assembly", "error", removeErr)
		}
	}()
	command := "assemble-resource"
	if plan.Kind == "PARENT_ARCHIVE" {
		command = "parent-archive"
	}
	var output struct {
		SHA256    string `json:"sha256"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	if err = s.Runtime.Call(ctx, command, map[string]any{
		"files": facts, "locators": locators,
		"paths": plan.Paths, "outputPath": absolute,
	}, &output); err != nil {
		return Blob{}, wrap(err)
	}
	reader, err := s.Storage.Read(key)
	if err != nil {
		return Blob{}, wrap(err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			slog.Error("close resource assembly", "error", closeErr)
		}
	}()
	file, err := s.Storage.Write(ctx, "games", run.Run.GameID, reader, 8*1024*1024*1024)
	if err != nil {
		return Blob{}, wrap(err)
	}
	if file.SHA256 != output.SHA256 || file.Size != output.SizeBytes {
		return Blob{}, fmt.Errorf("assembled resource integrity: %w", model.ErrInvalid)
	}
	return Blob{
		ID: key[len("temporary/"):], Filename: "game.zip", Key: file.Key, LogicalPath: "game.zip",
		SHA256: file.SHA256, SizeBytes: file.Size, MediaType: "application/zip",
	}, nil
}
