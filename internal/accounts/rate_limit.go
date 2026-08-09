package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

const (
	rateLimitWindow = 15 * time.Minute
	rateLimitBlock  = 15 * time.Minute
)

var ErrRateLimited = errors.New("AUTH_RATE_LIMITED")

type RateLimitError struct {
	retryAfterSeconds int
}

func (err *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (err *RateLimitError) Unwrap() error { return ErrRateLimited }

func RateLimitRetryAfter(err error) int {
	var limited *RateLimitError
	if errors.As(err, &limited) && limited.retryAfterSeconds > 0 {
		return limited.retryAfterSeconds
	}
	return 1
}

type rateLimitSubject struct {
	scope     string
	subject   string
	threshold int64
}

func retryAfterSeconds(blockedUntil, now int64) int {
	remaining := blockedUntil - now
	if remaining <= 0 {
		return 1
	}
	return int((remaining + int64(time.Second/time.Millisecond) - 1) / int64(time.Second/time.Millisecond))
}

func canonicalLoginSubject(input string) string {
	if username, err := authn.NormalizeUsername(input); err == nil {
		return username
	}
	return strings.ToLower(norm.NFC.String(input))
}

func (service *Service) checkRateLimits(ctx context.Context, subjects ...rateLimitSubject) error {
	now := service.now().UTC().UnixMilli()
	maximumRetry := 0
	for _, subject := range subjects {
		digest := service.credentials.RateLimitSubject(subject.scope, subject.subject)
		var blockedUntil sql.NullInt64
		err := service.database.QueryRowContext(ctx, `
SELECT blocked_until_ms FROM auth_rate_limits WHERE scope=? AND subject_hash=?
`, subject.scope, digest[:]).Scan(&blockedUntil)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("check authentication rate limit: %w", err)
		}
		if blockedUntil.Valid && blockedUntil.Int64 > now {
			maximumRetry = max(maximumRetry, retryAfterSeconds(blockedUntil.Int64, now))
		}
	}
	if maximumRetry > 0 {
		return &RateLimitError{retryAfterSeconds: maximumRetry}
	}
	return nil
}

func (service *Service) recordRateLimitFailures(ctx context.Context, subjects ...rateLimitSubject) error {
	now := service.now().UTC().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin authentication rate-limit update: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM auth_rate_limits WHERE rowid IN (
  SELECT rowid FROM auth_rate_limits
  WHERE updated_at_ms<? AND (blocked_until_ms IS NULL OR blocked_until_ms<=?) LIMIT 100
)
`, now-int64(24*time.Hour/time.Millisecond), now); err != nil {
		return fmt.Errorf("clean authentication rate limits: %w", err)
	}
	maximumRetry := 0
	for _, subject := range subjects {
		retry, updateErr := service.recordRateLimitFailure(ctx, transaction, subject, now)
		if updateErr != nil {
			return updateErr
		}
		maximumRetry = max(maximumRetry, retry)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit authentication rate limits: %w", err)
	}
	if maximumRetry > 0 {
		return &RateLimitError{retryAfterSeconds: maximumRetry}
	}
	return nil
}

func (service *Service) recordRateLimitFailure(
	ctx context.Context,
	transaction *sql.Tx,
	subject rateLimitSubject,
	now int64,
) (int, error) {
	digest := service.credentials.RateLimitSubject(subject.scope, subject.subject)
	var windowStarted, failures int64
	var blockedUntil sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
SELECT window_started_at_ms,failure_count,blocked_until_ms
FROM auth_rate_limits WHERE scope=? AND subject_hash=?
`, subject.scope, digest[:]).Scan(&windowStarted, &failures, &blockedUntil)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read authentication rate limit: %w", err)
	}
	if blockedUntil.Valid && blockedUntil.Int64 > now {
		return retryAfterSeconds(blockedUntil.Int64, now), nil
	}
	if errors.Is(err, sql.ErrNoRows) || now-windowStarted >= rateLimitWindow.Milliseconds() {
		windowStarted, failures, blockedUntil = now, 0, sql.NullInt64{}
	}
	failures++
	if failures >= subject.threshold {
		blockedUntil = sql.NullInt64{Int64: now + rateLimitBlock.Milliseconds(), Valid: true}
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO auth_rate_limits(
  scope,subject_hash,window_started_at_ms,failure_count,blocked_until_ms,updated_at_ms
) VALUES(?,?,?,?,?,?)
ON CONFLICT(scope,subject_hash) DO UPDATE SET
  window_started_at_ms=excluded.window_started_at_ms,
  failure_count=excluded.failure_count,
  blocked_until_ms=excluded.blocked_until_ms,
  updated_at_ms=excluded.updated_at_ms
`, subject.scope, digest[:], windowStarted, failures, blockedUntil, now)
	if err != nil {
		return 0, fmt.Errorf("update authentication rate limit: %w", err)
	}
	if blockedUntil.Valid {
		return retryAfterSeconds(blockedUntil.Int64, now), nil
	}
	return 0, nil
}

func (service *Service) clearRateLimit(ctx context.Context, subject rateLimitSubject) error {
	digest := service.credentials.RateLimitSubject(subject.scope, subject.subject)
	if _, err := service.database.ExecContext(ctx, `
DELETE FROM auth_rate_limits WHERE scope=? AND subject_hash=?
`, subject.scope, digest[:]); err != nil {
		return fmt.Errorf("clear authentication rate limit: %w", err)
	}
	return nil
}

func (service *Service) LoginRateLimited(
	ctx context.Context,
	username, password, clientIP string,
) (Session, error) {
	account := rateLimitSubject{
		scope: "LOGIN_ACCOUNT", subject: canonicalLoginSubject(username), threshold: 5,
	}
	ip := rateLimitSubject{scope: "LOGIN_IP", subject: clientIP, threshold: 30}
	if err := service.checkRateLimits(ctx, account, ip); err != nil {
		return Session{}, err
	}
	session, err := service.Login(ctx, username, password)
	if errors.Is(err, ErrAuthentication) {
		if rateErr := service.recordRateLimitFailures(ctx, account, ip); rateErr != nil {
			return Session{}, rateErr
		}
		return Session{}, err
	}
	if err != nil {
		return Session{}, err
	}
	if err := service.clearRateLimit(ctx, account); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (service *Service) InitializeRateLimited(
	ctx context.Context,
	request InitializeRequest,
	clientIP string,
) (Session, error) {
	subject := rateLimitSubject{scope: "SETUP_IP", subject: clientIP, threshold: 5}
	if err := service.checkRateLimits(ctx, subject); err != nil {
		return Session{}, err
	}
	session, err := service.Initialize(ctx, request)
	if rateLimitedSetupFailure(err) {
		if rateErr := service.recordRateLimitFailures(ctx, subject); rateErr != nil {
			return Session{}, rateErr
		}
	}
	return session, err
}

func (service *Service) InspectAccountLinkRateLimited(
	ctx context.Context,
	expectedKind, token, clientIP string,
) (LinkInspection, error) {
	subject := rateLimitSubject{scope: "LINK_IP", subject: clientIP, threshold: 20}
	if err := service.checkRateLimits(ctx, subject); err != nil {
		return LinkInspection{}, err
	}
	result, err := service.InspectAccountLink(ctx, expectedKind, token)
	if errors.Is(err, ErrAccountLinkUnavailable) {
		if rateErr := service.recordRateLimitFailures(ctx, subject); rateErr != nil {
			return LinkInspection{}, rateErr
		}
	}
	return result, err
}

func (service *Service) AcceptInvitationRateLimited(
	ctx context.Context,
	request AcceptInvitationRequest,
	clientIP string,
) (Session, error) {
	subject := rateLimitSubject{scope: "LINK_IP", subject: clientIP, threshold: 20}
	if err := service.checkRateLimits(ctx, subject); err != nil {
		return Session{}, err
	}
	result, err := service.AcceptInvitation(ctx, request)
	if rateLimitedLinkFailure(err) {
		if rateErr := service.recordRateLimitFailures(ctx, subject); rateErr != nil {
			return Session{}, rateErr
		}
	}
	return result, err
}

func (service *Service) CompletePasswordResetRateLimited(
	ctx context.Context,
	request CompletePasswordResetRequest,
	clientIP string,
) (PasswordResetResult, error) {
	subject := rateLimitSubject{scope: "LINK_IP", subject: clientIP, threshold: 20}
	if err := service.checkRateLimits(ctx, subject); err != nil {
		return PasswordResetResult{}, err
	}
	result, err := service.CompletePasswordReset(ctx, request)
	if rateLimitedLinkFailure(err) {
		if rateErr := service.recordRateLimitFailures(ctx, subject); rateErr != nil {
			return PasswordResetResult{}, rateErr
		}
	}
	return result, err
}

func rateLimitedSetupFailure(err error) bool {
	return errors.Is(err, ErrInitializationProof) || credentialInputFailure(err)
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
