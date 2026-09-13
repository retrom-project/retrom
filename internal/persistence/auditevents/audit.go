// Package auditevents contains the relational audit-event writer shared by
// application adapters that already own a transaction.
package auditevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/dbexec"
)

type Event struct {
	ID, ActorKind, Action, ResourceType, ResourceID string
	ActorUserID, ActorLabel                         any
	Before, After                                   any
	RequestID                                       any
	CreatedAtMS                                     int64
}

func Insert(ctx context.Context, executor dbexec.Executor, event Event) error {
	before, err := nullableJSON(event.Before)
	if err != nil {
		return fmt.Errorf("auditevents: encode before: %w", err)
	}
	after, err := nullableJSON(event.After)
	if err != nil {
		return fmt.Errorf("auditevents: encode after: %w", err)
	}
	_, err = executor.ExecContext(ctx, `
INSERT INTO audit_events(
 id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'{}',?,?)
`, event.ID, event.ActorKind, event.ActorUserID, event.ActorLabel, event.Action, event.ResourceType,
		event.ResourceID, before, after, event.RequestID, event.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("auditevents: insert: %w", err)
	}
	return nil
}

func nullableJSON(value any) (any, error) {
	if value == nil {
		return nil, nil //nolint:nilnil // nil stores a SQL NULL audit projection.
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal audit JSON: %w", err)
	}
	return string(encoded), nil
}

var _ dbexec.Executor = (*sql.Tx)(nil)
