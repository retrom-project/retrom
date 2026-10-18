package authn

import "time"

const (
	// SessionIdleLifetime is renewed by each successful authentication.
	SessionIdleLifetime = 3 * 24 * time.Hour
	// SessionAbsoluteLifetime caps both the session and its browser cookie.
	SessionAbsoluteLifetime = 7 * 24 * time.Hour
)
