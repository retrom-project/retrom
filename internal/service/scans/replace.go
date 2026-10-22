package scans

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

func (s *Service) Replace(ctx context.Context, p model.Principal, id, sourcePath string,
	version int64,
) (model.GameDetail, error) {
	if err := p.Admin(); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	if version < 1 {
		return model.GameDetail{}, model.ErrInvalid
	}
	old, err := s.Repository.GameDetail(ctx, p.User.ID, id, "published")
	if err != nil {
		return old, wrap(err)
	}
	directory, err := s.Repository.Directory(ctx, old.Game.PlatformInstanceID)
	if err != nil {
		return old, wrap(err)
	}
	root, name, err := s.Sources.OpenContent(sourcePath)
	if err != nil {
		return old, wrap(err)
	}
	defer closeRoot(root)
	operation := uuid.NewString()
	defer s.removeStaging(operation)
	files, err := s.content(ctx, root, operation, []string{filepath.ToSlash(name)}, directory, nil)
	if err != nil {
		return old, err
	}
	files, err = s.normalize(ctx, operation, directory.PlatformID, files)
	if err != nil {
		return old, err
	}
	config, err := s.configure(ctx, directory, files)
	if err != nil {
		return old, err
	}
	config, err = retainDOSProgram(old.RuntimeConfig, config)
	if err != nil {
		return old, err
	}
	prepared, err := s.Runtime.Identity(ctx, directory, config, files, "")
	if err != nil {
		return old, wrap(err)
	}
	physical, err := s.finalizeContent(ctx, id, directory, config, files)
	if err != nil {
		return old, err
	}
	game := model.PreparedGame{
		ID: id, Input: model.GameInput{Version: version, RuntimeConfig: config},
		Files: physical, ContentHash: prepared.ROMHash,
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.ReplaceFiles(ctx,
			id, game, s.Now().UnixMilli()))
	})
	if err = s.finishReplacement(ctx, id, physical, err); err != nil {
		return old, err
	}
	result, err := s.Repository.GameDetail(ctx, p.User.ID, id, "published")
	return result, wrap(err)
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
