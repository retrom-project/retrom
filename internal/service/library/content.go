package library

import (
	"context"
	"errors"
	"unicode/utf8"

	"retrom/internal/model"
)

func (s *Service) ContentPage(ctx context.Context, p model.Principal, id, after, prefix string,
	limit int,
) (model.ContentPage, error) {
	if err := p.Admin(); err != nil {
		return model.ContentPage{}, wrap(err)
	}
	if len(after) > 1024 || len(prefix) > 1024 || !utf8.ValidString(after) || !utf8.ValidString(prefix) ||
		limit < 1 || limit > 100 {
		return model.ContentPage{}, model.ErrInvalid
	}
	err := s.Repository.GameAccessible(ctx, id, "pending_review")
	if errors.Is(err, model.ErrNotFound) {
		err = s.Repository.GameAccessible(ctx, id, "published")
	}
	if err != nil {
		return model.ContentPage{}, wrap(err)
	}
	page, err := s.Repository.GameContentPage(ctx, id, after, prefix, limit)
	return page, wrap(err)
}
