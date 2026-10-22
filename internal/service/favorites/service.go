package favorites

import (
	"context"
	"fmt"
	"strings"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/service/tags"

	"github.com/google/uuid"
)

type Service struct {
	Repository *persistence.Repository
	Now        func() time.Time
}

func (s *Service) List(ctx context.Context, p model.Principal, q model.Query) (model.Page[model.Game], error) {
	q, err := s.favoriteQuery(ctx, p, q)
	if err != nil {
		return model.Page[model.Game]{}, err
	}
	result, err := s.Repository.Games(ctx, p.User.ID, "published", q)
	return result, wrap(err)
}

// Letters reports where each title letter group starts in the same favorite list.
func (s *Service) Letters(ctx context.Context, p model.Principal, q model.Query) ([]model.TitleLetter, error) {
	q, err := s.favoriteQuery(ctx, p, q)
	if err != nil {
		return nil, err
	}
	result, err := s.Repository.GameLetters(ctx, p.User.ID, "published", q)
	return result, wrap(err)
}

func (s *Service) favoriteQuery(ctx context.Context, p model.Principal, q model.Query) (model.Query, error) {
	if q.Unclassified && q.FolderID != "" {
		return q, model.ErrInvalid
	}
	q.Favorite = true
	if q.FolderID != "" {
		if _, err := s.Repository.Folder(ctx, p.User.ID, q.FolderID); err != nil {
			return q, wrap(err)
		}
	}
	return q, nil
}

func (s *Service) Folders(ctx context.Context, p model.Principal) ([]model.Folder, error) {
	result, err := s.Repository.Folders(ctx, p.User.ID)
	return result, wrap(err)
}

func (s *Service) WriteFolder(ctx context.Context,
	p model.Principal,
	id,
	name string,
	version int64) (model.Folder,
	error,
) {
	name = strings.TrimSpace(name)
	if !tags.ValidName(name) {
		return model.Folder{}, model.ErrInvalid
	}
	if id == "" {
		id = uuid.NewString()
		version = 0
	} else if version < 1 {
		return model.Folder{}, model.ErrInvalid
	}
	if err := s.Repository.WriteFolder(ctx, p.User.ID, id, name, version, s.Now().UnixMilli()); err != nil {
		return model.Folder{}, wrap(err)
	}
	result, err := s.Repository.Folder(ctx, p.User.ID, id)
	return result, wrap(err)
}

func (s *Service) DeleteFolder(ctx context.Context, p model.Principal, id string) error {
	return wrap(s.Repository.Transaction(ctx,
		func(r *persistence.Repository) error {
			return wrap(r.DeleteFolder(ctx,
				p.User.ID,
				id))
		}))
}

func (s *Service) Set(ctx context.Context, p model.Principal, id string, folderIDs []string) error {
	if len(folderIDs) > 100 {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(folderIDs))
	for _, folder := range folderIDs {
		if seen[folder] {
			return model.ErrInvalid
		}
		seen[folder] = true
	}
	return wrap(s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.SetFavorite(ctx, p.User.ID, id, folderIDs, s.Now().UnixMilli()))
	}))
}

func (s *Service) Remove(ctx context.Context, p model.Principal, id string) error {
	return wrap(s.Repository.Transaction(ctx,
		func(r *persistence.Repository) error {
			return wrap(r.RemoveFavorite(ctx,
				p.User.ID,
				id))
		}))
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("favorite operation: %w", err)
}

func (s *Service) Membership(ctx context.Context, p model.Principal, id string) ([]string, error) {
	result, err := s.Repository.FavoriteFolders(ctx, p.User.ID, id)
	return result, wrap(err)
}
