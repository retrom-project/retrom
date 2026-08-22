package netplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

func (service *Service) Events(ctx context.Context, roomID string, afterID int64, limit int) ([]Event, error) {
	var exists int
	if err := service.database.QueryRowContext(
		ctx, `SELECT count(*) FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&exists); err != nil {
		return nil, serviceError("check event room", err)
	}
	if exists != 1 {
		return nil, ErrRoomNotFound
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT id,event_type,data_json,created_at_ms FROM netplay_events
WHERE room_id=? AND id>? ORDER BY id LIMIT ?
	`, roomID, afterID, limit)
	if err != nil {
		return nil, serviceError("list events", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var data string
		if err := rows.Scan(&event.ID, &event.EventType, &data, &event.CreatedAt); err != nil {
			return nil, serviceError("scan event", err)
		}
		if err := json.Unmarshal([]byte(data), &event.Data); err != nil {
			return nil, fmt.Errorf("netplay/event data: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, serviceError("list events", err)
	}
	return result, nil
}

func (service *Service) ExpireRooms(ctx context.Context) error {
	now := service.clock.Now().UnixMilli()
	candidates, err := service.passiveExpiryCandidates(ctx, now)
	if err != nil {
		return err
	}
	for _, item := range candidates {
		if err := service.expirePassiveRoom(ctx, item, now); err != nil {
			return err
		}
	}
	active, err := service.activeExpiryCandidates(ctx, now)
	if err != nil {
		return err
	}
	for _, item := range active {
		if err := service.endRoomSystem(ctx, item.roomID, item.reason); err != nil && !errors.Is(err, ErrRoomNotFound) {
			return fmt.Errorf("netplay/end timed out session: %w", err)
		}
	}
	return nil
}

type passiveExpiryCandidate struct {
	roomID    string
	profileID string
}

func (service *Service) passiveExpiryCandidates(
	ctx context.Context, now int64,
) ([]passiveExpiryCandidate, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,host_profile_id FROM netplay_rooms
WHERE state IN ('DRAFT','WAITING') AND expires_at_ms<=?
ORDER BY expires_at_ms,id LIMIT 100
`, now)
	if err != nil {
		return nil, fmt.Errorf("netplay/find expired rooms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	candidates := make([]passiveExpiryCandidate, 0, 100)
	for rows.Next() {
		var item passiveExpiryCandidate
		if err := rows.Scan(&item.roomID, &item.profileID); err != nil {
			return nil, serviceError("scan expired room", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, serviceError("find expired rooms", err)
	}
	return candidates, nil
}

func (service *Service) expirePassiveRoom(
	ctx context.Context, item passiveExpiryCandidate, now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("expire room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='EXPIRED',ended_at_ms=?,end_reason='HARD_EXPIRED',version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('DRAFT','WAITING') AND expires_at_ms<=?
`, now, now, item.roomID, now)
	if err != nil {
		return serviceError("expire room", err)
	}
	if affected, _ := result.RowsAffected(); affected == 1 {
		data := map[string]any{"schemaVersion": 1, "reason": "HARD_EXPIRED"}
		if err := appendEvent(
			ctx, transaction, item.roomID, nil, &item.profileID, nil, "ROOM_EXPIRED", data, now,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("expire room commit", err)
	}
	return nil
}

type activeExpiryCandidate struct {
	roomID string
	reason string
}

func (service *Service) activeExpiryCandidates(
	ctx context.Context, now int64,
) ([]activeExpiryCandidate, error) {
	activeRows, err := service.database.QueryContext(ctx, `
SELECT room.id,
  CASE WHEN room.state='STARTING' THEN 'START_TIMEOUT' ELSE 'HARD_EXPIRED' END
FROM netplay_rooms room
LEFT JOIN netplay_sessions session ON session.id=room.current_session_id
WHERE (room.state='STARTING' AND room.updated_at_ms<=?)
   OR (room.state='RUNNING' AND session.started_at_ms IS NOT NULL AND session.started_at_ms<=?)
ORDER BY room.updated_at_ms,room.id LIMIT 100
	`, now-(2*time.Minute).Milliseconds(), now-(8*time.Hour).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("netplay/find timed out sessions: %w", err)
	}
	defer func() { cleanup.Error("close", activeRows.Close()) }()
	active := make([]activeExpiryCandidate, 0, 100)
	for activeRows.Next() {
		var item activeExpiryCandidate
		if err := activeRows.Scan(&item.roomID, &item.reason); err != nil {
			return nil, serviceError("scan timed out session", err)
		}
		active = append(active, item)
	}
	if err := activeRows.Err(); err != nil {
		return nil, serviceError("find timed out sessions", err)
	}
	return active, nil
}

func (service *Service) mutateHostRoom(
	ctx context.Context,
	roomID, actorProfileID string,
	expectedVersion int64,
	allowedStates []string,
	mutation func(*sql.Tx, int64) error,
) (Room, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("mutate host room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var host, state string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT host_profile_id,state,version FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&host, &state, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, ErrRoomNotFound
		}
		return Room{}, serviceError("mutate host room state", err)
	}
	if host != actorProfileID {
		return Room{}, ErrForbidden
	}
	if version != expectedVersion {
		return Room{}, ErrPrecondition
	}
	if !slices.Contains(allowedStates, state) {
		return Room{}, ErrRoomConflict
	}
	if err := mutation(transaction, service.clock.Now().UnixMilli()); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("mutate host room commit", err)
	}
	return service.Room(ctx, roomID, actorProfileID)
}

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

func newV7() string {
	value, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return value.String()
}

func intPointer(value int) *int { return &value }

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stringPointerFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
