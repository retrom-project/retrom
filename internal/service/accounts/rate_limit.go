package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/accounts"
)

type RateLimitError struct{ retryAfterSeconds int }

func (err *RateLimitError) Error() string { return model.ErrRateLimited.Error() }
func (err *RateLimitError) Unwrap() error { return model.ErrRateLimited }
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
	repository model.RateLimitRepository
	hasher     model.RateLimitHasher
	now        func() time.Time
}

func NewLimiter(repository model.RateLimitRepository, hasher model.RateLimitHasher, now func() time.Time) *Limiter {
	return &Limiter{repository: repository, hasher: hasher, now: now}
}

func (limiter *Limiter) key(subject model.RateLimitSubject) model.RateLimitKey {
	return model.RateLimitKey{
		Scope:  subject.Scope,
		Digest: limiter.hasher.RateLimitSubject(subject.Scope, subject.Subject),
	}
}

func (limiter *Limiter) Check(ctx context.Context, subjects ...model.RateLimitSubject) error {
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

func (limiter *Limiter) Record(ctx context.Context, subjects ...model.RateLimitSubject) error {
	now := limiter.now().UnixMilli()
	entries := make([]model.RateLimitEntry, len(subjects))
	for i, subject := range subjects {
		entries[i] = model.RateLimitEntry{
			Key:       limiter.key(subject),
			Threshold: subject.Threshold,
		}
	}
	result, err := limiter.repository.CommitRecordRateLimit(ctx, model.RecordRateLimitCommand{
		Entries: entries,
		NowMS:   now,
	})
	if err != nil {
		return fmt.Errorf("commit authentication rate limits: %w", err)
	}
	if result.MaxRetryAfterMS > 0 {
		return limitedError(retryAfterSeconds(result.MaxRetryAfterMS, now))
	}
	return nil
}

func (limiter *Limiter) Clear(ctx context.Context, subject model.RateLimitSubject) error {
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
