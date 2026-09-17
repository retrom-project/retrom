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
type RateLimitRepository interface {
	Read(context.Context, RateLimitKey) (RateLimitBucket, bool, error)
	Clear(context.Context, RateLimitKey) error
	CommitWrite(context.Context, func(RateLimitRecords) error) error
}
type RateLimitHasher interface{ RateLimitSubject(string, string) [32]byte }
