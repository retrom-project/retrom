package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/authn"

	"golang.org/x/text/unicode/norm"
)

func canonicalLoginSubject(input string) string {
	if username, err := authn.NormalizeUsername(input); err == nil {
		return username
	}
	return strings.ToLower(norm.NFC.String(input))
}

func (service *Service) LoginRateLimited(
	ctx context.Context,
	username, password, clientIP string,
) (Session, error) {
	timing := &loginTiming{started: time.Now()}
	session, err := service.loginRateLimited(ctx, username, password, clientIP, timing)
	timing.report(ctx, err)
	return session, err
}

func (service *Service) loginRateLimited(
	ctx context.Context, username, password, clientIP string, timing *loginTiming,
) (Session, error) {
	account := RateLimitSubject{
		Scope: "LOGIN_ACCOUNT", Subject: canonicalLoginSubject(username), Threshold: 5,
	}
	ip := RateLimitSubject{Scope: "LOGIN_IP", Subject: clientIP, Threshold: 30}
	if err := service.modules.Limiter.Check(ctx, account, ip); err != nil {
		timing.limits = time.Since(timing.started)
		return Session{}, fmt.Errorf("authentication limit check: %w", err)
	}
	timing.limits = time.Since(timing.started)
	session, err := service.modules.Authentication.login(ctx, username, password, []RateLimitKey{
		service.modules.Limiter.key(account), service.modules.Limiter.key(ip),
	}, timing)
	if errors.Is(err, ErrAuthentication) {
		if rateErr := service.modules.Limiter.Record(ctx, account, ip); rateErr != nil {
			return Session{}, fmt.Errorf("authentication limit record: %w", rateErr)
		}
		return Session{}, err
	}
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (service *Service) InitializeRateLimited(
	ctx context.Context,
	request InitializeRequest,
	clientIP string,
) (Session, error) {
	subject := RateLimitSubject{Scope: "SETUP_IP", Subject: clientIP, Threshold: 5}
	if err := service.modules.Limiter.Check(ctx, subject); err != nil {
		return Session{}, fmt.Errorf("authentication limit check: %w", err)
	}
	session, err := service.Initialize(ctx, request)
	if credentialInputFailure(err) {
		if rateErr := service.modules.Limiter.Record(ctx, subject); rateErr != nil {
			return Session{}, fmt.Errorf("authentication limit record: %w", rateErr)
		}
	}
	return session, err
}

func (service *Service) InspectAccountLinkRateLimited(
	ctx context.Context,
	expectedKind, token, clientIP string,
) (LinkInspection, error) {
	subject := RateLimitSubject{Scope: "LINK_IP", Subject: clientIP, Threshold: 20}
	if err := service.modules.Limiter.Check(ctx, subject); err != nil {
		return LinkInspection{}, fmt.Errorf("authentication limit check: %w", err)
	}
	result, err := service.InspectAccountLink(ctx, expectedKind, token)
	if errors.Is(err, ErrAccountLinkUnavailable) {
		if rateErr := service.modules.Limiter.Record(ctx, subject); rateErr != nil {
			return LinkInspection{}, fmt.Errorf("authentication limit record: %w", rateErr)
		}
	}
	return result, err
}

func (service *Service) AcceptInvitationRateLimited(
	ctx context.Context,
	request AcceptInvitationRequest,
	clientIP string,
) (Session, error) {
	subject := RateLimitSubject{Scope: "LINK_IP", Subject: clientIP, Threshold: 20}
	if err := service.modules.Limiter.Check(ctx, subject); err != nil {
		return Session{}, fmt.Errorf("authentication limit check: %w", err)
	}
	result, err := service.AcceptInvitation(ctx, request)
	if rateLimitedLinkFailure(err) {
		if rateErr := service.modules.Limiter.Record(ctx, subject); rateErr != nil {
			return Session{}, fmt.Errorf("authentication limit record: %w", rateErr)
		}
	}
	return result, err
}

func (service *Service) CompletePasswordResetRateLimited(
	ctx context.Context,
	request CompletePasswordResetRequest,
	clientIP string,
) (PasswordResetResult, error) {
	subject := RateLimitSubject{Scope: "LINK_IP", Subject: clientIP, Threshold: 20}
	if err := service.modules.Limiter.Check(ctx, subject); err != nil {
		return PasswordResetResult{}, fmt.Errorf("authentication limit check: %w", err)
	}
	result, err := service.CompletePasswordReset(ctx, request)
	if rateLimitedLinkFailure(err) {
		if rateErr := service.modules.Limiter.Record(ctx, subject); rateErr != nil {
			return PasswordResetResult{}, fmt.Errorf("authentication limit record: %w", rateErr)
		}
	}
	return result, err
}

func rateLimitedLinkFailure(err error) bool {
	return errors.Is(err, ErrAccountLinkUnavailable) || errors.Is(err, ErrUsernameUnavailable) ||
		credentialInputFailure(err)
}

func credentialInputFailure(err error) bool {
	var password *authn.PasswordError
	return errors.As(err, &password) || errors.Is(err, authn.ErrUsernameInvalid) ||
		errors.Is(err, authn.ErrDisplayInvalid)
}
