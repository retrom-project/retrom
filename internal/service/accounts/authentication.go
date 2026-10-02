package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/authn"
)

var (
	ErrAuthentication       = errors.New("AUTHENTICATION_FAILED")
	ErrAuthenticationNeeded = errors.New("AUTHENTICATION_REQUIRED")
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
	timing := &loginTiming{started: time.Now()}
	session, err := service.login(ctx, username, password, nil, timing)
	timing.report(ctx, err)
	return session, err
}

func (service *Authentication) login(
	ctx context.Context, username, password string, limits []RateLimitKey, timing *loginTiming,
) (Session, error) {
	credential, err := service.verify(ctx, username, password, timing)
	if err != nil {
		return Session{}, err
	}
	material, err := service.mint()
	if err != nil {
		return Session{}, fmt.Errorf("prepare login session: %w", err)
	}
	var now int64
	waitStarted := time.Now()
	var writeStarted time.Time
	err = service.repository.WithWrite(ctx, func(scope AuthScope) error {
		writeStarted = time.Now()
		timing.writerWait = writeStarted.Sub(waitStarted)
		now = service.now().UnixMilli()
		if err := checkLoginLimits(ctx, scope.Limits, limits, now); err != nil {
			return err
		}
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
		for _, key := range limits {
			if key.Scope == "LOGIN_ACCOUNT" {
				if err := scope.Limits.Clear(ctx, key); err != nil {
					return fmt.Errorf("clear successful login limit: %w", err)
				}
			}
		}
		return nil
	})
	if writeStarted.IsZero() {
		timing.writerWait = time.Since(waitStarted)
	} else {
		timing.transaction = time.Since(writeStarted)
	}
	if err != nil {
		return Session{}, fmt.Errorf("commit login session: %w", err)
	}
	return material.View(credential.User, credential.ProfileID, credential.SessionVersion, now), nil
}

func (service *Authentication) verify(
	ctx context.Context,
	usernameInput, passwordInput string,
	timing *loginTiming,
) (LoginCredential, error) {
	username, usernameErr := authn.NormalizeUsername(usernameInput)
	password, passwordErr := authn.NormalizeLoginPassword(passwordInput)
	var credential LoginCredential
	var found bool
	var lookupErr error
	readStarted := time.Now()
	if usernameErr == nil {
		credential, found, lookupErr = service.repository.Credential(ctx, username)
	}
	timing.credential = time.Since(readStarted)
	encoded := credential.PasswordHash
	if !found || lookupErr != nil {
		encoded = service.dummy
	}
	if passwordErr != nil {
		password = strings.Repeat("x", 24)
	}
	verifyStarted := time.Now()
	verified, err := service.verifier.Verify(ctx, password, encoded)
	timing.password = time.Since(verifyStarted)
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
	err := service.repository.WithWrite(ctx, func(scope AuthScope) error {
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
