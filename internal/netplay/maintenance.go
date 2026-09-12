package netplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

func appendEvent(
	ctx context.Context,
	transaction *sql.Tx,
	roomID string,
	sessionID, profileID *string,
	playerNo *int,
	eventType string,
	data map[string]any,
	now int64,
) error {
	if data == nil {
		data = map[string]any{"schemaVersion": 1}
	}
	encoded, err := json.Marshal(data)
	if err != nil || len(encoded) > 4096 {
		return errEventData
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,?,?,?,?,?,?)
`, roomID, sessionID, profileID, playerNo, eventType, string(encoded), now); err != nil {
		return fmt.Errorf("netplay/append event: %w", err)
	}
	return nil
}
