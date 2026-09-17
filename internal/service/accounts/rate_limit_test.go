package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/accounts"
)

type limitMemory struct {
	values    map[model.RateLimitKey]model.RateLimitBucket
	lateError error
	writes    int
	cleared   model.RateLimitKey
}

func (memory *limitMemory) Read(_ context.Context, key model.RateLimitKey) (model.RateLimitBucket, bool, error) {
	value, found := memory.values[key]
	return value, found, nil
}

func (memory *limitMemory) Write(_ context.Context, value model.RateLimitBucket) error {
	memory.writes++
	memory.values[value.Key] = value
	return nil
}
func (memory *limitMemory) Prune(context.Context, int64, int64) error { return nil }
func (memory *limitMemory) Clear(_ context.Context, key model.RateLimitKey) error {
	memory.cleared = key
	return nil
}

func (memory *limitMemory) CommitWrite(_ context.Context, work func(model.RateLimitRecords) error) error {
	if err := work(memory); err != nil {
		return err
	}
	return memory.lateError
}

type limitHasher struct{}

func (limitHasher) RateLimitSubject(scope, subject string) [32]byte {
	var value [32]byte
	copy(value[:], scope+"/"+subject)
	return value
}

func TestRateLimitThresholdAndExpiryUseInjectedClock(t *testing.T) {
	now := time.UnixMilli(100)
	memory := &limitMemory{values: map[model.RateLimitKey]model.RateLimitBucket{}}
	limiter := NewLimiter(memory, limitHasher{}, func() time.Time { return now })
	subject := model.RateLimitSubject{Scope: "LOGIN_ACCOUNT", Subject: "alice", Threshold: 2}
	if err := limiter.Record(t.Context(), subject); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Record(t.Context(), subject); !errors.Is(err, model.ErrRateLimited) || RateLimitRetryAfter(err) != 900 {
		t.Fatalf("threshold: %v", err)
	}
	writes := memory.writes
	now = now.Add(time.Millisecond)
	if err := limiter.Record(t.Context(), subject); RateLimitRetryAfter(err) != 900 {
		t.Fatalf("ceil retry: %v", err)
	}
	if memory.writes != writes {
		t.Fatal("blocked request extended block or incremented failures")
	}
	now = now.Add(15 * time.Minute)
	if err := limiter.Record(t.Context(), subject); err != nil {
		t.Fatalf("expired window: %v", err)
	}
	for _, bucket := range memory.values {
		if bucket.Failures != 1 || bucket.BlockedUntil != nil {
			t.Fatalf("window not reset: %+v", bucket)
		}
	}
}

func TestRateLimitCommitErrorIsNotReportedAsThrottle(t *testing.T) {
	memory := &limitMemory{values: map[model.RateLimitKey]model.RateLimitBucket{}, lateError: context.Canceled}
	err := NewLimiter(memory, limitHasher{}, time.Now).Record(t.Context(), model.RateLimitSubject{Scope: "LOGIN_ACCOUNT", Subject: "alice", Threshold: 1})
	if !errors.Is(err, context.Canceled) || errors.Is(err, model.ErrRateLimited) {
		t.Fatalf("lost commit failure: %v", err)
	}
}

func TestRateLimitCheckUsesLongestBlockAndClearUsesHashedSubject(t *testing.T) {
	memory := &limitMemory{values: map[model.RateLimitKey]model.RateLimitBucket{}}
	limiter := NewLimiter(memory, limitHasher{}, func() time.Time { return time.UnixMilli(100) })
	subjects := []model.RateLimitSubject{{Scope: "LOGIN_ACCOUNT", Subject: "alice", Threshold: 2}, {Scope: "LOGIN_IP", Subject: "192.0.2.1", Threshold: 30}}
	for i, subject := range subjects {
		key := model.RateLimitKey{Scope: subject.Scope, Digest: (limitHasher{}).RateLimitSubject(subject.Scope, subject.Subject)}
		until := int64(1100 + i*1000)
		memory.values[key] = model.RateLimitBucket{Key: key, BlockedUntil: &until}
	}
	if err := limiter.Check(t.Context(), subjects...); !errors.Is(err, model.ErrRateLimited) || RateLimitRetryAfter(err) != 2 {
		t.Fatalf("combined block: %v", err)
	}
	if err := limiter.Clear(t.Context(), subjects[0]); err != nil {
		t.Fatal(err)
	}
	if memory.cleared.Scope != "LOGIN_ACCOUNT" || memory.cleared.Digest != (limitHasher{}).RateLimitSubject("LOGIN_ACCOUNT", "alice") {
		t.Fatalf("cleared bucket: %+v", memory.cleared)
	}
}
