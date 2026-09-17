package accounts

import "context"

type RateLimitSubject struct {
	Scope, Subject string
	Threshold      int64
}
type RateLimitKey struct {
	Scope  string
	Digest [32]byte
}
type RateLimitBucket struct {
	Key                     RateLimitKey
	WindowStarted, Failures int64
	BlockedUntil            *int64
	UpdatedAt               int64
}
type RateLimitReader interface {
	Read(context.Context, RateLimitKey) (RateLimitBucket, bool, error)
}
type RateLimitRecords interface {
	Read(context.Context, RateLimitKey) (RateLimitBucket, bool, error)
	Write(context.Context, RateLimitBucket) error
	Prune(context.Context, int64, int64) error
}

// RecordRateLimitCommand captures all inputs for recording rate limit failures.
type RecordRateLimitCommand struct {
	Entries []RateLimitEntry
	NowMS   int64
}

// RateLimitEntry represents a single subject to record in a rate limit command.
type RateLimitEntry struct {
	Key       RateLimitKey
	Threshold int64
}

// RecordRateLimitResult is the outcome of a rate limit recording.
type RecordRateLimitResult struct {
	MaxRetryAfterMS int64
}

type RateLimitRepository interface {
	Read(context.Context, RateLimitKey) (RateLimitBucket, bool, error)
	Clear(context.Context, RateLimitKey) error
	CommitRecordRateLimit(context.Context, RecordRateLimitCommand) (RecordRateLimitResult, error)
}
type RateLimitHasher interface{ RateLimitSubject(string, string) [32]byte }
