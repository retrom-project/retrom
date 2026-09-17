package accounts

// ValidSession is a pure predicate that checks whether a session snapshot
// represents a valid, non-revoked, non-expired session at the given time.
func ValidSession(snapshot SessionSnapshot, now int64) bool {
	return snapshot.RevokedAt == nil && snapshot.Status == "ENABLED" &&
		snapshot.UserVersion == snapshot.SessionVersion &&
		now < snapshot.IdleExpiry && now < snapshot.AbsoluteExpiry
}
