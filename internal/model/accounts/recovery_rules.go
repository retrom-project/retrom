package accounts

// Recoverable is a pure predicate that checks whether a user is eligible for
// offline admin recovery (must be an admin and not deleted).
func Recoverable(target RecoveryTarget) bool {
	return target.Role == "ADMIN" && target.Status != "DELETED"
}
