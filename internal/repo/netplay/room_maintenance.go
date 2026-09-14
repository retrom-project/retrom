package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type RoomMaintenance struct{ database *sql.DB }

func NewRoomMaintenance(database *sql.DB) *RoomMaintenance { return &RoomMaintenance{database} }
func (repository *RoomMaintenance) WithMaintenance(
	ctx context.Context,
	work func(netplay.MaintenanceWriter) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin room maintenance: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(roomMaintenanceRecords{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("netplay/commit room maintenance: %w", err)
	}
	return nil
}

type roomMaintenanceRecords struct{ executor dbexec.Executor }

func (repository *RoomMaintenance) Passive(
	ctx context.Context,
	cutoffs netplay.ExpiryCutoffs,
) ([]netplay.ExpiryCandidate, error) {
	return repository.candidates(ctx, `
SELECT id,host_profile_id,state,version,current_session_id FROM netplay_rooms
WHERE state IN ('DRAFT','WAITING') AND expires_at_ms<=?
ORDER BY expires_at_ms,id LIMIT ?`, cutoffs.Now, cutoffs.Limit)
}

func (repository *RoomMaintenance) Active(
	ctx context.Context,
	cutoffs netplay.ExpiryCutoffs,
) ([]netplay.ExpiryCandidate, error) {
	return repository.candidates(ctx, `
SELECT room.id,room.host_profile_id,room.state,room.version,room.current_session_id
FROM netplay_rooms room LEFT JOIN netplay_sessions session ON session.id=room.current_session_id
WHERE (room.state='STARTING' AND room.updated_at_ms<=?)
OR (room.state='RUNNING' AND session.started_at_ms IS NOT NULL AND session.started_at_ms<=?)
ORDER BY room.updated_at_ms,room.id LIMIT ?`, cutoffs.StartingBefore, cutoffs.RunningBefore, cutoffs.Limit)
}

func (repository *RoomMaintenance) candidates(
	ctx context.Context,
	query string,
	args ...any,
) ([]netplay.ExpiryCandidate, error) {
	rows, err := repository.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("netplay/read expiry candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	candidates := make([]netplay.ExpiryCandidate, 0, 100)
	for rows.Next() {
		var candidate netplay.ExpiryCandidate
		if err := rows.Scan(
			&candidate.RoomID,
			&candidate.HostID,
			&candidate.State,
			&candidate.Version,
			&candidate.SessionID,
		); err != nil {
			return nil, fmt.Errorf("netplay/scan expiry candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/iterate expiry candidates: %w", err)
	}
	return candidates, nil
}

func (records roomMaintenanceRecords) Expire(ctx context.Context, plan netplay.ExpiryPlan) error {
	result, err := recordstore.UpdateNetplayRooms(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `state='EXPIRED',ended_at_ms=?,end_reason='HARD_EXPIRED',version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=? AND version=? AND state IN ('DRAFT','WAITING') AND expires_at_ms<=?`,
				Args:  []any{plan.Before.RoomID, plan.Before.Version, plan.Now},
			},
			Values: []any{plan.Now, plan.Now},
		},
	)
	if err != nil {
		return fmt.Errorf("netplay/expire idle room: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("netplay/expired room count: %w", err)
	}
	if count == 0 {
		return nil
	}
	_, err = records.executor.ExecContext(
		ctx,
		`
INSERT INTO netplay_events(room_id,profile_id,event_type,data_json,created_at_ms)
VALUES(?,?,'ROOM_EXPIRED','{"schemaVersion":1,"reason":"HARD_EXPIRED"}',?)`,
		plan.Before.RoomID,
		plan.Before.HostID,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("netplay/append expiry event: %w", err)
	}
	return nil
}
