package netplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
)

type RoomEvents struct{ database *sql.DB }

func NewRoomEvents(database *sql.DB) *RoomEvents { return &RoomEvents{database} }
func (repository *RoomEvents) Page(
	ctx context.Context,
	roomID string,
	after int64,
	limit int,
) (netplay.RoomEventPage, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return netplay.RoomEventPage{}, fmt.Errorf("netplay/begin event read: %w", err)
	}
	defer dbexec.Rollback(tx)
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM netplay_rooms WHERE id=?)`, roomID).Scan(
		&exists,
	); err != nil {
		return netplay.RoomEventPage{}, fmt.Errorf("netplay/read event room: %w", err)
	}
	if !exists {
		return netplay.RoomEventPage{}, nil
	}
	events, err := readRoomEvents(ctx, tx, roomID, after, limit)
	if err != nil {
		return netplay.RoomEventPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return netplay.RoomEventPage{}, fmt.Errorf("netplay/commit event read: %w", err)
	}
	return netplay.RoomEventPage{Exists: true, Events: events}, nil
}

func readRoomEvents(
	ctx context.Context,
	executor dbexec.Executor,
	roomID string,
	after int64,
	limit int,
) ([]netplay.Event, error) {
	rows, err := executor.QueryContext(
		ctx,
		`SELECT id,event_type,data_json,created_at_ms FROM netplay_events WHERE room_id=? AND id>? ORDER BY id LIMIT ?`,
		roomID,
		after,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("netplay/read room events: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	events := make([]netplay.Event, 0, limit)
	for rows.Next() {
		var event netplay.Event
		var data string
		if err := rows.Scan(&event.ID, &event.EventType, &data, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("netplay/scan room event: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &event.Data); err != nil {
			return nil, fmt.Errorf("netplay/decode room event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/iterate room events: %w", err)
	}
	return events, nil
}
