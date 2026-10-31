package library

import (
	"context"
	"unicode/utf8"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

// KirikiriEntryCandidates projects runtime entry rules independently of the selected entry.
func (s *Service) KirikiriEntryCandidates(ctx context.Context, p model.Principal, id, after, prefix string,
	limit int,
) (model.ProjectEntryPage, error) {
	result := model.ProjectEntryPage{Entries: []string{}}
	if err := p.Admin(); err != nil {
		return result, wrap(err)
	}
	if len(after) > 1024 || len(prefix) > 1024 || !utf8.ValidString(after) || !utf8.ValidString(prefix) ||
		limit < 1 || limit > 100 {
		return result, model.ErrInvalid
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
	err = s.Runtime.Call(ctx, "kirikiri-entry-candidates", map[string]any{
		"platformId": directory.PlatformID, "files": runtimeclient.Files(detail.ContentFiles), "locators": locators,
		"after": after, "prefix": prefix, "limit": limit,
	}, &result)
	if err != nil {
		return result, wrap(err)
	}
	if result.Entries == nil || len(result.Entries) > limit {
		return model.ProjectEntryPage{}, model.ErrUnavailable
	}
	return result, nil
}
