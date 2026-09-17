package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/capability/security/authn"
)

type Authentication struct {
	repository AuthRepository
	verifier   PasswordVerifier
	mint       SessionMinter
	dummy      string
	now        func() time.Time
}

func NewAuthentication(
	repository AuthRepository,
	verifier PasswordVerifier,
	mint SessionMinter,
	dummy string,
	now func() time.Time,
) *Authentication {
	return &Authentication{repository: repository, verifier: verifier, mint: mint, dummy: dummy, now: now}
}

func (service *Authentication) Login(ctx context.Context, username, password string) (Session, error) {
	credential, err := service.verify(ctx, username, password)
	if err != nil {
		return Session{}, err
	}
	material, err := service.mint()
	if err != nil {
		return Session{}, fmt.Errorf("prepare login session: %w", err)
	}
	var now int64
	err = service.repository.CommitWrite(ctx, func(scope AuthScope) error {
		now = service.now().UnixMilli()
		if err := scope.Write.Login(
			ctx,
			credential,
			material.Record(
				credential.User.UserID,
				credential.SessionVersion,
				now,
			),
		); err != nil {
			return fmt.Errorf("create login session: %w", err)
		}
		return nil
	})
	if err != nil {
		return Session{}, fmt.Errorf("commit login session: %w", err)
	}
	return material.View(credential.User, credential.ProfileID, credential.SessionVersion, now), nil
}

func (service *Authentication) verify(
	ctx context.Context,
	usernameInput, passwordInput string,
) (LoginCredential, error) {
	username, usernameErr := authn.NormalizeUsername(usernameInput)
	password, passwordErr := authn.NormalizeLoginPassword(passwordInput)
	var credential LoginCredential
	var found bool
	var lookupErr error
	if usernameErr == nil {
		credential, found, lookupErr = service.repository.Credential(ctx, username)
	}
	encoded := credential.PasswordHash
	if !found || lookupErr != nil {
		encoded = service.dummy
	}
	if passwordErr != nil {
		password = strings.Repeat("x", 24)
	}
	verified, err := service.verifier.Verify(ctx, password, encoded)
	if err != nil {
		return LoginCredential{}, errors.Join(lookupErr, fmt.Errorf("verify login credential: %w", err))
	}
	if lookupErr != nil {
		return LoginCredential{}, fmt.Errorf("read login credential: %w", lookupErr)
	}
	if usernameErr != nil || passwordErr != nil || !found || !verified || credential.Status != "ENABLED" {
		return LoginCredential{}, ErrAuthentication
	}
	return credential, nil
}

func (service *Authentication) Logout(ctx context.Context, id string) error {
	err := service.repository.CommitWrite(ctx, func(scope AuthScope) error {
		if err := scope.Write.Revoke(ctx, id, service.now().UnixMilli()); err != nil {
			return fmt.Errorf("revoke authentication session: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit session logout: %w", err)
	}
	return nil
}
