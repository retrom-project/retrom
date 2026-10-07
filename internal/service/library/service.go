package library

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
)

type Service struct {
	Repository     *persistence.Repository
	Runtime        *runtimeclient.Client
	Storage        *storage.Store
	ValidateConfig func(context.Context, string, json.RawMessage, []model.GameFile) error
	Now            func() time.Time
}

func (s *Service) List(ctx context.Context,
	p model.Principal,
	status string,
	q model.Query) (model.Page[model.Game],
	error,
) {
	if status != "published" {
		if err := p.Admin(); err != nil {
			return model.Page[model.Game]{}, wrap(err)
		}
	}
	items, err := s.Repository.Games(ctx, p.User.ID, status, q)
	return items, wrap(err)
}

func (s *Service) Detail(ctx context.Context, p model.Principal, id, status string) (model.GameDetail, error) {
	if status != "published" {
		if err := p.Admin(); err != nil {
			return model.GameDetail{}, wrap(err)
		}
	}
	result, err := s.Repository.GameDetail(ctx, p.User.ID, id, status)
	return result, wrap(err)
}

func (s *Service) Validate(ctx context.Context, input model.GameInput, files []model.GameFile) error {
	if err := model.ValidateGameFields(input); err != nil {
		return wrap(err)
	}
	directory, err := s.Repository.Directory(ctx, input.PlatformInstanceID)
	if err != nil {
		return wrap(err)
	}
	if !directory.Enabled {
		return model.ErrInvalid
	}
	return wrap(s.ValidateConfig(ctx, directory.PlatformID, input.RuntimeConfig, files))
}

func (s *Service) Update(ctx context.Context,
	p model.Principal,
	id,
	status string,
	input model.GameInput) (model.GameDetail,
	error,
) {
	if err := p.Admin(); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	files, err := s.Repository.GameFiles(ctx, id)
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	if err = s.Validate(ctx, input, files); err != nil {
		return model.GameDetail{}, err
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		directory, readErr := r.Directory(ctx, input.PlatformInstanceID)
		if readErr != nil {
			return wrap(readErr)
		}
		if !directory.Enabled {
			return model.ErrConflict
		}
		return wrap(r.UpdateGame(ctx, id, p.User.ID, status, input, s.Now().UnixMilli()))
	})
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	return s.Detail(ctx, p, id, status)
}

func (s *Service) Approve(ctx context.Context, p model.Principal, id string, version int64) (model.GameDetail, error) {
	if err := p.Admin(); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	err := s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		status, current, lockErr := r.LockGame(ctx, id)
		if lockErr != nil {
			return wrap(lockErr)
		}
		if status == "published" {
			return nil
		}
		if status != "pending_review" || current != version {
			return model.ErrConflict
		}
		detail, readErr := r.GameDetail(ctx, p.User.ID, id, status)
		if readErr != nil {
			return wrap(readErr)
		}
		if len(detail.Files) == 0 || !model.Text(detail.Game.Title, 300) {
			return model.ErrInvalid
		}
		return wrap(r.TransitionGame(ctx, id, status, "published", version, s.Now().UnixMilli()))
	})
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	return s.Detail(ctx, p, id, "published")
}

func (s *Service) Delete(ctx context.Context, p model.Principal, id, status string, version int64) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.TransitionGame(ctx, id, status, "deleted", version, s.Now().UnixMilli()))
}

func (s *Service) Media(ctx context.Context, p model.Principal, id, mediaID string) (string, error) {
	if _, err := s.Repository.Game(ctx, p.User.ID, id, "published"); err != nil {
		if p.Admin() != nil {
			return "", wrap(err)
		}
		if _, err = s.Repository.Game(ctx, p.User.ID, id, "pending_review"); err != nil {
			return "", wrap(err)
		}
	}
	key, err := s.Repository.MediaKey(ctx, id, mediaID)
	return key, wrap(err)
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("library operation: %w", err)
}
