package launch

import "time"

type accessPolicy struct {
	now     func() time.Time
	matches MatchCapability
}

func (policy accessPolicy) active(session SessionRecord) bool {
	return session.State == "ACTIVE" && session.HardExpiresAtMS > policy.now().UnixMilli()
}

func (policy accessPolicy) authorized(session SessionRecord, capability string) bool {
	return policy.active(session) && policy.matches != nil && policy.matches(capability, session.CredentialHash)
}
