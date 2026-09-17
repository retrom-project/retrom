package accounts

import "time"

const (
	RateLimitWindow = 15 * time.Minute
	RateLimitBlock  = 15 * time.Minute
)

// NextRateLimitBucket computes the new bucket state after recording a failure.
// This is a pure function with no I/O.
func NextRateLimitBucket(
	current RateLimitBucket,
	found bool,
	key RateLimitKey,
	threshold, now int64,
) RateLimitBucket {
	if !found || now-current.WindowStarted >= RateLimitWindow.Milliseconds() {
		current = RateLimitBucket{Key: key, WindowStarted: now}
	}
	current.Failures++
	current.UpdatedAt = now
	if current.Failures >= threshold {
		until := now + RateLimitBlock.Milliseconds()
		current.BlockedUntil = &until
	}
	return current
}
