package tagging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func writeAudit(
	ctx context.Context,
	records AuditRecords,
	actorUserID, action, resourceType, resourceID string,
	before, after, diff any,
	now int64,
) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("tagging: create audit id: %w", err)
	}
	event := AuditEvent{
		ID:           id.String(),
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
			return fmt.Errorf("tagging: encode audit: %w", err)
		}
		*targets[index] = encoded
	}
	return repositoryError("write audit", records.Record(ctx, event))
}
