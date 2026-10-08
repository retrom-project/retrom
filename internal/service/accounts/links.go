package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

type LinkInspection struct {
	Kind        string  `json:"kind"`
	Role        string  `json:"role"`
	ExpiresAtMs int64   `json:"expiresAtMs"`
	Username    *string `json:"username"`
}

func (s *Service) CreateLink(ctx context.Context,
	p model.Principal,
	kind,
	role,
	target string,
	hours int64) (model.AccountLink,
	error,
) {
	if err := p.Admin(); err != nil {
		return model.AccountLink{}, wrap(err)
	}
	if !model.OneOf(kind, "invitation", "password_reset") || hours < 1 || hours > 168 {
		return model.AccountLink{}, model.ErrInvalid
	}
	if kind == "invitation" && !model.OneOf(role, "admin", "user") {
		return model.AccountLink{}, model.ErrInvalid
	}
	if kind == "password_reset" {
		if err := s.Repository.RequireUser(ctx, target); err != nil {
			return model.AccountLink{}, wrap(err)
		}
	}
	link := model.AccountLink{
		ID: uuid.NewString(), Kind: kind, Role: role, TargetUserID: target,
		ExpiresAtMs: s.Now().UnixMilli() + hours*3600000, Version: 1,
	}
	err := s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.CreateLink(ctx, link, p.User.ID, s.Now().UnixMilli()))
	})
	if err != nil {
		return link, wrap(err)
	}
	route := "/register"
	if kind == "password_reset" {
		route = "/reset-password"
	}
	link.URL = s.PublicOrigin + route + "#token=" + s.signLink(link.ID)
	return link, nil
}

func (s *Service) Inspect(ctx context.Context, token, ip string) (LinkInspection, error) {
	if err := s.rate(ctx, "account-link", ip, 20); err != nil {
		return LinkInspection{}, err
	}
	id, err := s.linkID(token)
	if err != nil {
		return LinkInspection{}, err
	}
	record, err := s.Repository.Link(ctx, id, false)
	if err != nil {
		return LinkInspection{}, wrap(err)
	}
	if !s.usable(record) {
		return LinkInspection{}, model.ErrNotFound
	}
	result := LinkInspection{Kind: record.Link.Kind, Role: record.Link.Role, ExpiresAtMs: record.Link.ExpiresAtMs}
	if record.Link.Kind == "password_reset" {
		user, readErr := s.Repository.User(ctx, record.Link.TargetUserID)
		if readErr != nil {
			return result, wrap(readErr)
		}
		result.Username = &user.Username
	}
	return result, nil
}

func (s *Service) Accept(ctx context.Context, token, username, name, password, ip string) (model.User, string, error) {
	if err := s.rate(ctx, "account-link", ip, 20); err != nil {
		return model.User{}, "", err
	}
	id, err := s.linkID(token)
	if err != nil {
		return model.User{}, "", err
	}
	user, hash, err := s.NewUser(ctx, username, name, password, "user")
	if err != nil {
		return user, "", err
	}
	sessionToken, err := randomToken()
	if err != nil {
		return user, "", err
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		record, readErr := r.Link(ctx, id, true)
		if readErr != nil {
			return wrap(readErr)
		}
		if !s.usable(record) || record.Link.Kind != "invitation" {
			return model.ErrNotFound
		}
		user.Role = record.Link.Role
		if writeErr := r.CreateUser(ctx, user, hash, s.Now().UnixMilli()); writeErr != nil {
			return wrap(writeErr)
		}
		if writeErr := r.ConsumeLink(ctx, id, user.ID, s.Now().UnixMilli()); writeErr != nil {
			return wrap(writeErr)
		}
		return wrap(r.CreateSession(ctx, uuid.NewString(), user.ID, sessionToken, s.Now().UnixMilli()))
	})
	return user, sessionToken, wrap(err)
}

func (s *Service) Reset(ctx context.Context, token, password, ip string) error {
	if err := s.rate(ctx, "account-link", ip, 20); err != nil {
		return err
	}
	id, err := s.linkID(token)
	if err != nil {
		return err
	}
	record, err := s.Repository.Link(ctx, id, false)
	if err != nil {
		return wrap(err)
	}
	user, err := s.Repository.User(ctx, record.Link.TargetUserID)
	if err != nil {
		return wrap(err)
	}
	_, hash, err := s.NewUser(ctx, user.Username, user.DisplayName, password, user.Role)
	if err != nil {
		return err
	}
	return wrap(s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		current, readErr := r.Link(ctx, id, true)
		if readErr != nil {
			return wrap(readErr)
		}
		if !s.usable(current) || current.Link.Kind != "password_reset" {
			return model.ErrNotFound
		}
		if writeErr := r.ChangePassword(ctx, user.ID, hash, s.Now().UnixMilli()); writeErr != nil {
			return wrap(writeErr)
		}
		return wrap(r.ConsumeLink(ctx, id, user.ID, s.Now().UnixMilli()))
	}))
}

func (s *Service) signLink(id string) string {
	hash := hmac.New(sha256.New, s.LinkKey)
	hash.Write([]byte(id))
	return id + "." + base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

func (s *Service) linkID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(token), []byte(s.signLink(parts[0]))) {
		return "", model.ErrNotFound
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return "", model.ErrNotFound
	}
	return parts[0], nil
}

func (s *Service) usable(record persistence.LinkRecord) bool {
	return record.ConsumedAt == nil && record.RevokedAt == nil && record.Link.ExpiresAtMs > s.Now().UnixMilli()
}

func (s *Service) RevokeLink(ctx context.Context, p model.Principal, id string, version int64) error {
	if version < 1 {
		return model.ErrInvalid
	}
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.RevokeLink(ctx, id, p.User.ID, version, s.Now().UnixMilli()))
}
