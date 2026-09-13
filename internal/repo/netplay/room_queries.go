package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/service/netplay"
)

type RoomQueries struct{ database *sql.DB }

func NewRoomQueries(database *sql.DB) *RoomQueries { return &RoomQueries{database: database} }
func roomIDsQuery(filter netplay.RoomFilter) (string, []any) {
	terminalClause := "room.state NOT IN ('ENDED','EXPIRED') AND member.left_at_ms IS NULL"
	if filter.View == "recent" {
		terminalClause = "room.state IN ('ENDED','EXPIRED') AND room.ended_at_ms>=?"
	}
	query := `
SELECT DISTINCT room.id
FROM netplay_rooms room
JOIN netplay_room_members member ON member.room_id=room.id
WHERE member.profile_id=? AND ` + terminalClause
	arguments := []any{filter.ProfileID}
	if filter.View == "recent" {
		arguments = append(arguments, filter.RecentSinceMS)
	}
	if filter.AfterRoomID != "" {
		query += ` AND (room.updated_at_ms < ? OR (room.updated_at_ms = ? AND room.id < ?))`
		arguments = append(arguments, filter.AfterUpdatedAtMS, filter.AfterUpdatedAtMS, filter.AfterRoomID)
	}
	query += `
ORDER BY room.updated_at_ms DESC,room.id DESC LIMIT ?
`
	arguments = append(arguments, filter.Limit)
	return query, arguments
}

func (repository *RoomQueries) RoomIDs(ctx context.Context, filter netplay.RoomFilter) ([]string, error) {
	query, arguments := roomIDsQuery(filter)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("netplay/list rooms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	ids := make([]string, 0, filter.Limit)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("netplay/list rooms: %w", err)
		}
		ids = append(ids, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/list rooms: %w", err)
	}
	return ids, nil
}
