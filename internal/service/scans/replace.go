package scans

import (
	"context"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

func (s *Service) Replace(ctx context.Context, p model.Principal, id, rootID, relative string,
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
	root, err := s.Sources.Open(rootID, ".")
	if err != nil {
		return old, wrap(err)
	}
	defer closeRoot(root)
	files, err := s.content(ctx, root, id, []string{relative}, directory)
	if err != nil {
		return old, err
	}
	files, err = s.normalize(ctx, id, directory.PlatformID, files)
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
	game := model.PreparedGame{
		ID: id, Input: model.GameInput{Version: version, RuntimeConfig: config},
		Files: files, ContentHash: prepared.ROMHash,
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.ReplaceFiles(ctx,
			id, game, s.Now().UnixMilli()))
	})
	if err != nil {
		return old, wrap(err)
	}
	result, err := s.Repository.GameDetail(ctx, p.User.ID, id, "published")
	return result, wrap(err)
}
