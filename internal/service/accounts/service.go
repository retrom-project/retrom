package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/temporary"
)

type Service struct {
	Repository   *persistence.Repository
	Redis        *temporary.Redis
	Hasher       *authn.PasswordHasher
	Blocklist    authn.Blocklist
	LinkKey      []byte
	PublicOrigin string
	Now          func() time.Time
}

func (s *Service) NewUser(ctx context.Context, username, name, password, role string) (model.User, string, error) {
	user := model.User{ID: uuid.NewString(), Role: role, Status: "active", Version: 1, CreatedAtMs: s.Now().UnixMilli()}
	var err error
	user.Username, err = authn.NormalizeUsername(username)
	if err != nil {
		return user, "", fmt.Errorf("username: %w", model.ErrInvalid)
	}
	user.DisplayName, err = authn.NormalizeDisplayName(name)
	if err != nil {
		return user, "", fmt.Errorf("display name: %w", model.ErrInvalid)
	}
	normalized, err := authn.ValidatePassword(password, password, user.Username, user.DisplayName, s.Blocklist)
	if err != nil {
		return user, "", fmt.Errorf("password policy: %w", model.ErrInvalid)
	}
	hash, err := s.Hasher.Hash(ctx, normalized)
	return user, hash, wrap(err)
}

func (s *Service) Initialize(ctx context.Context, username, name, password, ip string) (model.User, string, error) {
	if err := s.rate(ctx, "initialize", ip, 5); err != nil {
		return model.User{}, "", err
	}
	user, hash, err := s.NewUser(ctx, username, name, password, "admin")
	if err != nil {
		return user, "", wrap(err)
	}
	token, err := randomToken()
	if err != nil {
		return user, "", wrap(err)
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		if createErr := r.Initialize(ctx, user, hash, s.Now().UnixMilli()); createErr != nil {
			return wrap(createErr)
		}
		return wrap(r.CreateSession(ctx, uuid.NewString(), user.ID, token, s.Now().UnixMilli()))
	})
	return user, token, wrap(err)
}

func (s *Service) Login(ctx context.Context, username, password, ip string) (model.User, string, error) {
	normalized, err := authn.NormalizeUsername(username)
	if err != nil {
		return model.User{}, "", model.ErrUnauthorized
	}
	if err = s.rate(ctx, "login-ip", ip, 30); err != nil {
		return model.User{}, "", err
	}
	if err = s.rate(ctx, "login-user", normalized, 5); err != nil {
		return model.User{}, "", err
	}
	user, hash, err := s.Repository.Credential(ctx, normalized)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return user, "", model.ErrUnauthorized
		}
		return user, "", wrap(err)
	}
	normalizedPassword, err := authn.NormalizeLoginPassword(password)
	if err != nil {
		return user, "", model.ErrUnauthorized
	}
	match, err := s.Hasher.Verify(ctx, normalizedPassword, hash)
	if err != nil {
		return user, "", wrap(err)
	}
	if !match || user.Status != "active" {
		return user, "", model.ErrUnauthorized
	}
	token, err := randomToken()
	if err != nil {
		return user, "", wrap(err)
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		if fenceErr := r.FenceCredential(ctx, user.ID, hash); fenceErr != nil {
			return wrap(fenceErr)
		}
		if createErr := r.CreateSession(ctx, uuid.NewString(), user.ID, token, s.Now().UnixMilli()); createErr != nil {
			return wrap(createErr)
		}
		return wrap(r.LoginTime(ctx, user.ID, s.Now().UnixMilli()))
	})
	if err != nil {
		return user, "", wrap(err)
	}
	if err = s.Redis.Delete(ctx, "limit:"+subject("login-user", normalized)); err != nil {
		return user, "", wrap(err)
	}
	now := s.Now().UnixMilli()
	user.LastLoginAtMs = &now
	return user, token, nil
}

func (s *Service) BootstrapTest(ctx context.Context) error {
	initialized, err := s.Repository.Initialized(ctx)
	if err != nil || initialized {
		return wrap(err)
	}
	user := model.User{ID: uuid.NewString(), Username: "test", DisplayName: "Test administrator", Role: "admin"}
	hash, err := s.Hasher.Hash(ctx, "test")
	if err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.Initialize(ctx, user, hash, s.Now().UnixMilli()))
	}))
}

func (s *Service) ChangePassword(ctx context.Context, p model.Principal, current, newPassword string) (string, error) {
	_, hash, err := s.Repository.Credential(ctx, p.User.Username)
	if err != nil {
		return "", wrap(err)
	}
	normalized, err := authn.NormalizeLoginPassword(current)
	if err != nil {
		return "", wrap(model.Invalid("currentPassword"))
	}
	match, err := s.Hasher.Verify(ctx, normalized, hash)
	if err != nil {
		return "", wrap(err)
	}
	if !match {
		return "", wrap(model.Invalid("currentPassword"))
	}
	_, next, err := s.NewUser(ctx, p.User.Username, p.User.DisplayName, newPassword, p.User.Role)
	if err != nil {
		return "", wrap(err)
	}
	token, err := randomToken()
	if err != nil {
		return "", wrap(err)
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		if fenceErr := r.FenceCredential(ctx, p.User.ID, hash); fenceErr != nil {
			return wrap(fenceErr)
		}
		if err = r.ChangePassword(ctx, p.User.ID, next, s.Now().UnixMilli()); err != nil {
			return wrap(err)
		}
		return wrap(r.CreateSession(ctx, uuid.NewString(), p.User.ID, token, s.Now().UnixMilli()))
	})
	return token, wrap(err)
}

func (s *Service) UpdateUser(ctx context.Context, p model.Principal, user model.User) (model.User, error) {
	if err := p.Admin(); err != nil {
		return model.User{}, wrap(err)
	}
	if user.ID == p.User.ID && (user.Role != p.User.Role || user.Status != p.User.Status) {
		return user, model.ErrForbidden
	}
	name, err := authn.NormalizeDisplayName(user.DisplayName)
	if err != nil {
		return user, wrap(model.Invalid("displayName"))
	}
	validRole := model.OneOf(user.Role, "admin", "user")
	validStatus := model.OneOf(user.Status, "active", "disabled", "deleted")
	if !validRole || !validStatus || user.Version < 1 {
		return user, model.ErrInvalid
	}
	user.DisplayName = name
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.UpdateUser(ctx, user, s.Now().UnixMilli()))
	})
	if err != nil {
		return user, wrap(err)
	}
	updated, err := s.Repository.User(ctx, user.ID)
	return updated, wrap(err)
}

func (s *Service) rate(ctx context.Context, scope, value string, maximum int) error {
	return wrap(s.Redis.Limit(ctx, subject(scope, value), maximum))
}

func subject(scope, value string) string {
	hash := sha256.Sum256([]byte(value))
	return scope + ":" + hex.EncodeToString(hash[:])
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("account operation: %w", err)
}
