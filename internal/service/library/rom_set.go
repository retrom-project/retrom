package library

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

func (s *Service) ROMSetOptions(ctx context.Context, p model.Principal, id string, query model.ROMSetQuery,
) (model.ROMSetOptions, error) {
	if err := p.Admin(); err != nil {
		return model.ROMSetOptions{}, wrap(err)
	}
	detail, err := s.managedGame(ctx, p, id)
	if err != nil {
		return model.ROMSetOptions{}, err
	}
	directory, err := s.Repository.Directory(ctx, detail.Game.PlatformInstanceID)
	if err != nil {
		return model.ROMSetOptions{}, wrap(err)
	}
	result, err := s.Runtime.ROMSets(ctx, directory, detail.RuntimeConfig, detail.Files, query)
	return result, wrap(err)
}

// Runtime projects references; host manages the existing file lifecycle and transaction.
func (s *Service) reconcileConfig(ctx context.Context, directory model.Directory, config, previous json.RawMessage,
	files []model.GameFile,
) (json.RawMessage, []model.GameFile, []string, error) {
	if s.Runtime == nil {
		return config, files, nil, nil
	}
	projection, err := s.Runtime.ReconcileConfig(ctx, directory, config, previous, files)
	if err != nil {
		return nil, nil, nil, wrap(err)
	}
	removed := make(map[string]bool, len(projection.RemovedParentFiles))
	for _, key := range projection.RemovedParentFiles {
		removed[key] = true
	}
	retained := make([]model.GameFile, 0, len(files))
	retired := make([]string, 0, len(removed))
	for _, file := range files {
		if removed[file.LogicalKey] {
			retired = append(retired, file.ID)
		} else {
			retained = append(retained, file)
		}
	}
	return projection.Config, retained, retired, nil
}

func (s *Service) updateFileIdentity(ctx context.Context, r *persistence.Repository,
	id, hash string, retired []string, now int64,
) error {
	if len(retired) == 0 {
		return nil
	}
	return wrap(r.RetireParentFiles(ctx, id, hash, retired, now))
}
