package launch

import (
	model "retrom/internal/model/launch"
	"time"
)

type accessPolicy struct {
	now     func() time.Time
	matches model.MatchCapability
}

func (policy accessPolicy) active(session model.SessionRecord) bool {
	return session.State == "ACTIVE" && session.HardExpiresAtMS > policy.now().UnixMilli()
}

func (policy accessPolicy) authorized(session model.SessionRecord, capability string) bool {
	return policy.active(session) && policy.matches != nil && policy.matches(capability, session.CredentialHash)
}
