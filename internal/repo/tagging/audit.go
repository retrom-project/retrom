package tagging

import (
	"context"
	"fmt"

	"retrom/internal/model/tagging"
)

func (records auditRecords) Record(ctx context.Context, audit tagging.AuditEvent) error {
	_, err := records.database.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,?,?,?,?,?,?,NULL,?)
`, audit.ID, audit.ActorUserID, audit.Action, audit.ResourceType, audit.ResourceID,
		nullableJSON(audit.Before), nullableJSON(audit.After), nullableJSON(audit.Diff), audit.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("tagging: write audit: %w", err)
	}
	return nil
}

func nullableJSON(value []byte) any {
	if value == nil {
		return nil
	}
	return string(value)
}
