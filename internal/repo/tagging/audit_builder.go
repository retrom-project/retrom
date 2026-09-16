package tagging

import (
	"encoding/json"
	"fmt"

	"retrom/internal/model/tagging"
)

func buildAuditEvent(
	auditID, actorUserID, action, resourceType, resourceID string,
	before, after, diff any,
	now int64,
) tagging.AuditEvent {
	event := tagging.AuditEvent{
		ID:           auditID,
		ActorUserID:  actorUserID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		CreatedAtMS:  now,
	}
	targets := []*[]byte{&event.Before, &event.After, &event.Diff}
	for index, value := range []any{before, after, diff} {
		if value == nil {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			panic(fmt.Sprintf("tagging: encode audit %s: %v", action, err))
		}
		*targets[index] = encoded
	}
	return event
}
