package accounts

// LinkState computes the current state of an account link at the given time.
func LinkState(link AccountLink, now int64) string {
	switch {
	case link.ConsumedAtMS != nil:
		return "CONSUMED"
	case link.RevokedAtMS != nil:
		return "REVOKED"
	case now >= link.ExpiresAtMS:
		return "EXPIRED"
	default:
		return "ACTIVE"
	}
}
