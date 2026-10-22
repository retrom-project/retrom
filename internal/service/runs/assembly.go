package runs

import (
	"context"
	"log/slog"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"

	"github.com/google/uuid"
)

func (s *Service) assembled(ctx context.Context, run *Context, selected []Blob,
	plan runtimeclient.Resource,
) (Blob, error) {
	if reusablePackage(selected, plan) {
		file := selected[0]
		return Blob{
			ID: uuid.NewString(), Filename: "game.zip", LogicalPath: "game.zip", Key: file.Key,
			SHA256: file.PackageSHA256, SizeBytes: file.PackageSize,
			MediaType: "application/zip", PackageID: file.PackageID,
		}, nil
	}
	files := make([]model.ContentFile, 0, len(selected))
	for _, file := range selected {
		name := file.LogicalPath
		if mapped := plan.Paths[name]; mapped != "" {
			name = mapped
		}
		files = append(files, model.ContentFile{
			LogicalKey: name, StorageKey: file.Key, Offset: file.Offset,
			SHA256: file.SHA256, SizeBytes: file.SizeBytes,
		})
	}
	file, err := s.Storage.BuildTemporaryPackage(ctx, run.Run.ID, files)
	if err != nil {
		return Blob{}, wrap(err)
	}
	run.TemporaryKeys = append(run.TemporaryKeys, file.StorageKey)
	return Blob{
		ID: uuid.NewString(), Filename: "game.zip", LogicalPath: "game.zip", Key: file.StorageKey,
		SHA256: file.SHA256, SizeBytes: file.SizeBytes, MediaType: "application/zip",
	}, nil
}

func reusablePackage(selected []Blob, plan runtimeclient.Resource) bool {
	if len(selected) == 0 || selected[0].PackageID == "" {
		return false
	}
	first := selected[0]
	// Selected member count is checked against the frozen package facts, never the current index.
	if first.PackageMembers != len(selected) {
		return false
	}
	for _, file := range selected {
		if file.PackageID != first.PackageID || file.Key != first.Key || file.PackageSHA256 != first.PackageSHA256 ||
			file.PackageSize != first.PackageSize {
			return false
		}
		if mapped := plan.Paths[file.LogicalPath]; mapped != "" && mapped != file.LogicalPath {
			return false
		}
	}
	return true
}

func (s *Service) releaseTemporary(keys []string) {
	for _, key := range keys {
		if err := s.Storage.Remove(key); err != nil {
			slog.Error("remove run resource", "error", err)
		}
	}
}
