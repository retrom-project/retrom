package library

import (
	"context"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func (s *Service) RPGEntryCandidates(ctx context.Context, p model.Principal, id, engine string) (
	model.RPGEntryCandidates, error,
) {
	result := model.RPGEntryCandidates{Entries: []string{}}
	if err := p.Admin(); err != nil {
		return result, wrap(err)
	}
	detail, err := s.managedGame(ctx, p, id)
	if err != nil {
		return result, err
	}
	input := map[string]any{"config": detail.RuntimeConfig, "files": runtimeclient.Files(detail.ContentFiles)}
	if engine != "" {
		input["engine"] = engine
	}
	if err = s.Runtime.Call(ctx, "rpg-entry-candidates", input, &result); err != nil {
		return result, wrap(err)
	}
	if result.Entries == nil {
		return model.RPGEntryCandidates{Entries: []string{}}, model.ErrUnavailable
	}
	return result, nil
}
