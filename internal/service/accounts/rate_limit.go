package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	rateLimitWindow = 15 * time.Minute
	rateLimitBlock  = 15 * time.Minute
)

type RateLimitError struct{ retryAfterSeconds int }

func (err *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (err *RateLimitError) Unwrap() error { return ErrRateLimited }
func RateLimitRetryAfter(err error) int {
	var limited *RateLimitError
	if errors.As(err, &limited) && limited.retryAfterSeconds > 0 {
		return limited.retryAfterSeconds
	}
	return 1
}

func retryAfterSeconds(until, now int64) int {
	remaining := until - now
	if remaining <= 0 {
		return 1
	}
	return int((remaining + 999) / 1000)
}

type Limiter struct {
	repository RateLimitRepository
	hasher     RateLimitHasher
	now        func() time.Time
}

func NewLimiter(repository RateLimitRepository, hasher RateLimitHasher, now func() time.Time) *Limiter {
	return &Limiter{repository: repository, hasher: hasher, now: now}
}

func (limiter *Limiter) key(subject RateLimitSubject) RateLimitKey {
	return RateLimitKey{Scope: subject.Scope, Digest: limiter.hasher.RateLimitSubject(subject.Scope, subject.Subject)}
}

func (limiter *Limiter) Check(ctx context.Context, subjects ...RateLimitSubject) error {
	now := limiter.now().UnixMilli()
	maximum := 0
	for _, subject := range subjects {
		value, found, err := limiter.repository.Read(ctx, limiter.key(subject))
		if err != nil {
			return fmt.Errorf("read authentication rate limit: %w", err)
		}
		if found && value.BlockedUntil != nil && *value.BlockedUntil > now {
			maximum = max(maximum, retryAfterSeconds(*value.BlockedUntil, now))
		}
	}
	return limitedError(maximum)
}

func (limiter *Limiter) Record(ctx context.Context, subjects ...RateLimitSubject) error {
	maximum := 0
	err := limiter.repository.WithWrite(ctx, func(records RateLimitRecords) error {
		now := limiter.now().UnixMilli()
		if err := records.Prune(ctx, now-(24*time.Hour).Milliseconds(), now); err != nil {
			return fmt.Errorf("prune authentication limits: %w", err)
		}
		for _, subject := range subjects {
			retry, err := limiter.record(ctx, records, subject, now)
			if err != nil {
				return err
			}
			maximum = max(maximum, retry)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit authentication rate limits: %w", err)
	}
	return limitedError(maximum)
}

func (limiter *Limiter) record(
	ctx context.Context,
	records RateLimitRecords,
	subject RateLimitSubject,
	now int64,
) (int, error) {
	key := limiter.key(subject)
	value, found, err := records.Read(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("read authentication failure bucket: %w", err)
	}
	if found && value.BlockedUntil != nil && *value.BlockedUntil > now {
		return retryAfterSeconds(*value.BlockedUntil, now), nil
	}
	if !found || now-value.WindowStarted >= rateLimitWindow.Milliseconds() {
		value = RateLimitBucket{Key: key, WindowStarted: now}
	}
	value.Failures++
	value.UpdatedAt = now
	if value.Failures >= subject.Threshold {
		until := now + rateLimitBlock.Milliseconds()
		value.BlockedUntil = &until
	}
	if err := records.Write(ctx, value); err != nil {
		return 0, fmt.Errorf("record authentication failure bucket: %w", err)
	}
	if value.BlockedUntil != nil {
		return retryAfterSeconds(*value.BlockedUntil, now), nil
	}
	return 0, nil
}

func (limiter *Limiter) Clear(ctx context.Context, subject RateLimitSubject) error {
	if err := limiter.repository.Clear(ctx, limiter.key(subject)); err != nil {
		return fmt.Errorf("clear authentication bucket: %w", err)
	}
	return nil
}

func limitedError(retry int) error {
	if retry > 0 {
		return &RateLimitError{retryAfterSeconds: retry}
	}
	return nil
}
