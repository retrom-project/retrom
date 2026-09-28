// Package runtimesession owns the shared browser runtime credential and its lifetime.
package runtimesession

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

var ErrCredential = errors.New("RUNTIME_CREDENTIAL_INVALID")

const (
	LifetimeMS   int64 = 24 * 60 * 60 * 1000
	RenewalAgeMS int64 = LifetimeMS / 2
)

type Session struct {
	ID, AuthSessionID, UserID, ProfileID  string
	Token                                 string
	Refreshed                             bool
	Hash                                  [32]byte
	CreatedAtMS, RenewedAtMS, ExpiresAtMS int64
}

type Snapshot struct {
	Session                     Session
	Enabled, Revoked            bool
	UserVersion, SessionVersion int64
}

type Repository interface {
	Find(context.Context, [32]byte) (Snapshot, bool, error)
	Ensure(context.Context, string, Session, int64) (Snapshot, error)
	Renew(context.Context, string, int64, int64) (Snapshot, error)
	Runs(context.Context, string, string, int64) ([]string, error)
	FinishablePreview(context.Context, string, string, int64) (bool, error)
	ExtendRun(context.Context, string, string, int64, int64) error
}

type Environment struct {
	Now   func() time.Time
	NewID func() (string, error)
	Sign  func(string) (string, error)
}

type Service struct {
	repository  Repository
	environment Environment
}

func New(repository Repository, environment Environment) *Service {
	return &Service{repository: repository, environment: environment}
}

func (service *Service) Ensure(ctx context.Context, authSessionID string) (Session, error) {
	id, err := service.environment.NewID()
	if err != nil {
		return Session{}, fmt.Errorf("allocate runtime session: %w", err)
	}
	token, err := service.environment.Sign(id)
	if err != nil {
		return Session{}, fmt.Errorf("sign runtime session: %w", err)
	}
	hash, err := tokenDigest(token)
	if err != nil {
		return Session{}, err
	}
	now := service.environment.Now().UnixMilli()
	candidate := Session{
		ID: id, AuthSessionID: authSessionID, Hash: hash, CreatedAtMS: now,
		RenewedAtMS: now, ExpiresAtMS: now + LifetimeMS,
	}
	snapshot, err := service.repository.Ensure(ctx, authSessionID, candidate, now)
	if err != nil {
		return Session{}, fmt.Errorf("ensure runtime session: %w", err)
	}
	return service.accept(ctx, snapshot, true)
}

func (service *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	digest, err := tokenDigest(token)
	if err != nil {
		return Session{}, err
	}
	snapshot, found, err := service.repository.Find(ctx, digest)
	if err != nil {
		return Session{}, fmt.Errorf("read runtime session: %w", err)
	}
	if !found {
		return Session{}, ErrCredential
	}
	return service.accept(ctx, snapshot, true)
}

func (service *Service) accept(ctx context.Context, snapshot Snapshot, renew bool) (Session, error) {
	now := service.environment.Now().UnixMilli()
	if !snapshot.Enabled || snapshot.Revoked || snapshot.UserVersion != snapshot.SessionVersion ||
		snapshot.Session.ExpiresAtMS <= now {
		return Session{}, ErrCredential
	}
	if renew && now-snapshot.Session.RenewedAtMS > RenewalAgeMS {
		current, err := service.repository.Renew(ctx, snapshot.Session.ID, now, now+LifetimeMS)
		if err != nil {
			return Session{}, fmt.Errorf("renew runtime session: %w", err)
		}
		result, err := service.accept(ctx, current, false)
		result.Refreshed = err == nil
		return result, err
	}
	token, err := service.environment.Sign(snapshot.Session.ID)
	if err != nil {
		return Session{}, fmt.Errorf("recover runtime credential: %w", err)
	}
	snapshot.Session.Token = token
	return snapshot.Session, nil
}

func (service *Service) Runs(ctx context.Context, session Session, id string) ([]string, error) {
	runs, err := service.repository.Runs(ctx, session.ProfileID, id, service.environment.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read runtime runs: %w", err)
	}
	return runs, nil
}

func (service *Service) AuthorizeRun(ctx context.Context, session Session, id string) error {
	runs, err := service.Runs(ctx, session, id)
	if err != nil {
		return err
	}
	if len(runs) != 1 {
		return ErrCredential
	}
	if err := service.repository.ExtendRun(ctx, session.ProfileID, id,
		service.environment.Now().UnixMilli(), session.ExpiresAtMS); err != nil {
		return fmt.Errorf("extend active runtime run: %w", err)
	}
	return nil
}

func tokenDigest(token string) ([32]byte, error) {
	bytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(bytes) != 32 || base64.RawURLEncoding.EncodeToString(bytes) != token {
		return [32]byte{}, ErrCredential
	}
	return sha256.Sum256(bytes), nil
}

func (service *Service) AuthorizePreviewFinish(ctx context.Context, session Session, id string) error {
	allowed, err := service.repository.FinishablePreview(ctx, session.ProfileID, id, service.environment.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("authorize preview finish: %w", err)
	}
	if !allowed {
		return ErrCredential
	}
	return nil
}
