package scans

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

func (s *Service) prepareReplacement(ctx context.Context, task *replacementTask) error {
	id, version := task.session.Progress.GameID, task.version
	old, err := s.Repository.GameDetail(ctx, task.principal.User.ID, id, "published")
	if err != nil {
		return wrap(err)
	}
	if old.Game.Version != version {
		return model.ErrConflict
	}
	directory, err := s.Repository.Directory(ctx, old.Game.PlatformInstanceID)
	if err != nil {
		return wrap(err)
	}
	if !directory.Enabled {
		return model.ErrInvalid
	}
	game, err := s.buildReplacement(ctx, task, old, directory)
	if err != nil {
		return err
	}
	physical := game.Files
	if err = s.beginReplacementPublication(ctx, task, physical[0].ID,
		model.ContentReplacementResult{Version: version + 1, ContentHash: game.ContentHash}); err != nil {
		s.removePrepared(physical)
		return err
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.ReplaceFiles(ctx,
			id, game, s.Now().UnixMilli()))
	})
	if err = s.finishReplacement(ctx, id, physical, err); err != nil {
		return err
	}
	return nil
}

// buildReplacement performs source reads, normalization and physical publication
// outside the Game CAS transaction. All source content stays read-only.
func (s *Service) buildReplacement(ctx context.Context, task *replacementTask,
	old model.GameDetail, directory model.Directory,
) (model.PreparedGame, error) {
	root, name, err := s.Sources.OpenContent(task.sourcePath)
	if err != nil {
		return model.PreparedGame{}, wrap(err)
	}
	defer closeRoot(root)
	operation := task.session.Progress.ID
	defer s.removeStaging(operation)
	files, err := s.content(ctx, root, operation, []string{filepath.ToSlash(name)}, directory, nil)
	if err != nil {
		return model.PreparedGame{}, err
	}
	if err = s.replacementPhase(ctx, task, "normalizing"); err != nil {
		return model.PreparedGame{}, err
	}
	files, err = s.normalize(ctx, operation, directory.PlatformID, files)
	if err != nil {
		return model.PreparedGame{}, err
	}
	if err = s.replacementPhase(ctx, task, "configuring"); err != nil {
		return model.PreparedGame{}, err
	}
	config, err := s.configure(ctx, directory, files)
	if err != nil {
		return model.PreparedGame{}, err
	}
	config, err = retainDOSProgram(old.RuntimeConfig, config)
	if err != nil {
		return model.PreparedGame{}, err
	}
	prepared, err := s.Runtime.Identity(ctx, directory, config, files, "")
	if err != nil {
		return model.PreparedGame{}, wrap(err)
	}
	if err = s.replacementPhase(ctx, task, "packaging"); err != nil {
		return model.PreparedGame{}, err
	}
	physical, err := s.finalizeContent(ctx, task.session.Progress.GameID, directory, config, files)
	if err != nil {
		return model.PreparedGame{}, err
	}
	game := model.PreparedGame{
		ID: task.session.Progress.GameID, Input: model.GameInput{Version: task.version, RuntimeConfig: config},
		Files: physical, ContentHash: prepared.ROMHash,
	}
	return game, nil
}

func (s *Service) finishReplacement(ctx context.Context, id string, files []model.GameFile, commitErr error) error {
	if commitErr == nil {
		return nil
	}
	if errors.Is(commitErr, persistence.ErrCommitUncertain) {
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		committed, checkErr := s.Repository.ParentCommitted(bounded, id, files[0].ID)
		if checkErr != nil {
			return wrap(errors.Join(commitErr, checkErr))
		}
		if committed {
			return nil
		}
	}
	s.removePrepared(files)
	return wrap(commitErr)
}
