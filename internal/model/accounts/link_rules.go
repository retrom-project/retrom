package accounts

// ActiveLink reports whether a link record is an active link of the given kind.
func ActiveLink(record LinkRecord, found bool, kind string, now int64) bool {
	return found && record.Link.Kind == kind && LinkState(record.Link, now) == "ACTIVE"
}

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
