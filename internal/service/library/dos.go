package library

import (
	"context"
	"encoding/json"
	"errors"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func (s *Service) DOSEntryCandidates(ctx context.Context, p model.Principal, id, entryFile string) (
	model.DOSEntryCandidates, error,
) {
	result := model.DOSEntryCandidates{Entries: []string{}}
	if err := p.Admin(); err != nil {
		return result, wrap(err)
	}
	detail, err := s.Repository.GameRuntimeDetail(ctx, p.User.ID, id, "pending_review")
	if errors.Is(err, model.ErrNotFound) {
		detail, err = s.Repository.GameRuntimeDetail(ctx, p.User.ID, id, "published")
	}
	if err != nil {
		return result, wrap(err)
	}
	config, err := dosCandidateConfig(detail.RuntimeConfig, entryFile)
	if err != nil {
		return result, err
	}
	locators := make(map[string]runtimeclient.Locator, len(detail.ContentFiles))
	for _, file := range detail.ContentFiles {
		if s.Runtime.Locate == nil {
			return result, model.ErrUnavailable
		}
		location, locateErr := s.Runtime.Locate(file.StorageKey)
		if locateErr != nil {
			return result, model.ErrUnavailable
		}
		locators[file.LogicalKey] = runtimeclient.Locator{Path: location, Offset: file.Offset, SizeBytes: file.SizeBytes}
	}
	err = s.Runtime.Call(ctx, "dos-entry-candidates", map[string]any{
		"config": config, "files": runtimeclient.Files(detail.ContentFiles), "locators": locators,
	}, &result)
	if err != nil || result.Entries == nil {
		return model.DOSEntryCandidates{Entries: []string{}}, model.ErrUnavailable
	}
	return result, nil
}

func dosCandidateConfig(raw json.RawMessage, entryFile string) (map[string]any, error) {
	var config struct {
		Content struct {
			Kind      string `json:"kind"`
			EntryFile string `json:"entryFile"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.Content.Kind != "DOS_BUNDLE" {
		return nil, model.ErrInvalid
	}
	if entryFile == "" {
		entryFile = config.Content.EntryFile
	}
	return map[string]any{"content": map[string]any{"kind": "DOS_BUNDLE", "entryFile": entryFile}}, nil
}
