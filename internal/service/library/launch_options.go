package library

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

// LaunchOptions is read-only and accepts the editor's current draft. Format
// recognition and the resulting configurations remain owned by runtime.
func (s *Service) LaunchOptions(ctx context.Context, p model.Principal, id string,
	draft json.RawMessage,
) (model.LaunchOptions, error) {
	result := model.LaunchOptions{Items: []model.LaunchOption{}}
	if err := p.Admin(); err != nil {
		return result, wrap(err)
	}
	detail, err := s.managedGame(ctx, p, id)
	if err != nil {
		return result, err
	}
	directory, err := s.Repository.Directory(ctx, detail.Game.PlatformInstanceID)
	if err != nil {
		return result, wrap(err)
	}
	locators := make(map[string]runtimeclient.Locator, len(detail.ContentFiles))
	for _, file := range detail.ContentFiles {
		location, locateErr := s.Storage.Absolute(file.StorageKey)
		if locateErr != nil {
			return result, wrap(locateErr)
		}
		locators[file.LogicalKey] = runtimeclient.Locator{Path: location, Offset: file.Offset, SizeBytes: file.SizeBytes}
	}
	err = s.Runtime.Call(ctx, "launch-options", map[string]any{
		"platformId": directory.PlatformID, "config": draft,
		"files": runtimeclient.Files(detail.ContentFiles), "locators": locators,
	}, &result)
	if err != nil {
		return result, wrap(err)
	}
	if result.Items == nil {
		return model.LaunchOptions{}, model.ErrUnavailable
	}
	return result, nil
}
