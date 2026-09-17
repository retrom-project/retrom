package accounts

// PasswordAuthorized is a pure predicate that checks whether the given state
// allows the actor to change their password.
func PasswordAuthorized(state PasswordState, actor PasswordActor) bool {
	return state.SessionCurrent && state.Credential.Status == "ENABLED" &&
		state.Credential.SessionVersion == actor.SessionVersion
}
