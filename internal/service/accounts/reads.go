package accounts

import (
	"context"

	"retrom/internal/model"
)

func (s *Service) Initialized(ctx context.Context) (bool, error) {
	value, err := s.Repository.Initialized(ctx)
	return value, wrap(err)
}

func (s *Service) Authenticate(ctx context.Context, token string) (model.Principal, error) {
	value, err := s.Repository.Authenticate(ctx, token, s.Now().UnixMilli())
	return value, wrap(err)
}

func (s *Service) Logout(ctx context.Context, p model.Principal) error {
	return wrap(s.Repository.Logout(ctx, p.SessionID, s.Now().UnixMilli()))
}

func (s *Service) Users(ctx context.Context, p model.Principal, q model.Query) (model.Page[model.User], error) {
	if err := p.Admin(); err != nil {
		return model.Page[model.User]{}, wrap(err)
	}
	value, err := s.Repository.Users(ctx, q)
	return value, wrap(err)
}

func (s *Service) Links(ctx context.Context, p model.Principal,
	q model.Query,
) (model.Page[model.AccountLinkSummary], error) {
	if err := p.Admin(); err != nil {
		return model.Page[model.AccountLinkSummary]{}, wrap(err)
	}
	if q.Status != "" && !model.OneOf(q.Status, "active", "consumed", "revoked", "expired") {
		return model.Page[model.AccountLinkSummary]{}, model.ErrInvalid
	}
	value, err := s.Repository.Links(ctx, q, s.Now().UnixMilli())
	return value, wrap(err)
}
