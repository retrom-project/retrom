package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type RoomCreation struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewRoomCreation(database *sql.DB) *RoomCreation { return &RoomCreation{database: database} }

func (repository *RoomCreation) WithPreCommitHook(hook func() error) {
	repository.preCommitHook = hook
}

func (repository *RoomCreation) CommitRoomCreation(
	ctx context.Context, cmd netplay.RoomCreationCommand,
) (netplay.Room, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/begin room creation: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := roomCreationRecords{transaction}
	capacity, err := records.Capacity(ctx, cmd.Plan.HostID)
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/room capacity: %w", err)
	}
	if err := netplay.ValidateRoomCapacity(capacity, cmd.Maximum); err != nil {
		return netplay.Room{}, fmt.Errorf("validate room capacity: %w", err)
	}
	room, err := records.Insert(ctx, cmd.Plan)
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/persist room: %w", err)
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return netplay.Room{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/commit room creation: %w", err)
	}
	return room, nil
}

type roomCreationRecords struct{ executor dbexec.Executor }

func (records roomCreationRecords) Capacity(ctx context.Context, hostID string) (netplay.RoomCapacity, error) {
	var result netplay.RoomCapacity
	err := records.executor.QueryRowContext(ctx, `
SELECT count(*),EXISTS(SELECT 1 FROM netplay_rooms
WHERE host_profile_id=? AND state IN ('DRAFT','WAITING','STARTING','RUNNING'))
FROM netplay_rooms WHERE state IN ('DRAFT','WAITING','STARTING','RUNNING')
`, hostID).Scan(&result.Active, &result.HostActive)
	if err != nil {
		return netplay.RoomCapacity{}, fmt.Errorf("netplay/read room capacity: %w", err)
	}
	return result, nil
}

func (records roomCreationRecords) Insert(ctx context.Context, plan netplay.RoomCreationPlan) (netplay.Room, error) {
	result, err := recordstore.CreateNetplayRooms(ctx, records.executor, `
INSERT INTO netplay_rooms(id,host_profile_id,state,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'DRAFT',1,?,?,?)
ON CONFLICT(host_profile_id) WHERE state IN ('DRAFT','WAITING','STARTING','RUNNING') DO NOTHING
`, plan.RoomID, plan.HostID, plan.ExpiresAtMS, plan.Now, plan.Now)
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/insert room: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/insert room count: %w", err)
	}
	if count != 1 {
		return netplay.Room{}, netplay.ErrRoomConflict
	}
	if _, err := recordstore.CreateNetplayRoomMembers(ctx, records.executor, `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'HOST',1,0,1,?,?)
`, plan.MemberID, plan.RoomID, plan.HostID, plan.Now, plan.Now); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/insert host: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,NULL,?,1,'ROOM_CREATED',?,?)
`, plan.RoomID, plan.HostID, string(plan.Event), plan.Now); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/room created event: %w", err)
	}
	return loadRoomSnapshot(ctx, records.executor, plan.RoomID)
}
