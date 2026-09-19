package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type SessionStart struct{ database *sql.DB }

func NewSessionStart(database *sql.DB) *SessionStart { return &SessionStart{database: database} }

func (repository *SessionStart) InspectRoom(ctx context.Context, roomID, hostID string) (netplay.RoomControlSnapshot, error) {
	return roomControlRecords{repository.database}.Current(ctx, roomID, hostID)
}

type sessionStartRecords struct{ executor dbexec.Executor }

func (records sessionStartRecords) NextNumber(ctx context.Context, roomID string) (int, error) {
	var number int
	err := records.executor.QueryRowContext(
		ctx, `SELECT COALESCE(max(session_no),0)+1 FROM netplay_sessions WHERE room_id=?`, roomID,
	).Scan(&number)
	if err != nil {
		return 0, fmt.Errorf("netplay/read session number: %w", err)
	}
	return number, nil
}

func (records sessionStartRecords) Insert(ctx context.Context, plan netplay.SessionStartPlan) (netplay.Room, error) {
	result, err := recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
		Set: `version=version`, Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='WAITING'`,
			Args:  []any{plan.Before.RoomID, plan.Before.Version},
		},
	})
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return netplay.Room{}, err
	}
	if err := records.session(ctx, plan); err != nil {
		return netplay.Room{}, err
	}
	for _, member := range plan.Members {
		if err := records.participant(ctx, plan, member); err != nil {
			return netplay.Room{}, err
		}
	}
	result, err = recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
		Set: `state='STARTING',current_session_id=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{plan.Before.RoomID, plan.Before.Version},
		}, Values: []any{
			plan.SessionID,
			plan.Now,
		},
	})
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return netplay.Room{}, err
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,?,?,1,'SESSION_CREATED',?,?)
`, plan.Before.RoomID, plan.SessionID, plan.Before.HostID, string(plan.Event), plan.Now); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/session event: %w", err)
	}
	return loadRoomSnapshot(ctx, records.executor, plan.Before.RoomID)
}

func (records sessionStartRecords) session(ctx context.Context, plan netplay.SessionStartPlan) error {
	selected := plan.Profile.Selection
	_, err := recordstore.CreateNetplaySessions(
		ctx,
		records.executor,
		`
INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_id,provider_id,target_id,bundle_sha256,
netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,
authority_player_no,resync_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'PREPARING',?,?,?,?,?,?,?,?,?,?,1,0,1,?,?)
`,
		plan.SessionID,
		plan.Before.RoomID,
		plan.SessionNo,
		selected.GameID,
		selected.VariantID,
		plan.Profile.ProviderID,
		plan.Profile.TargetID,
		plan.Profile.BundleSHA256,
		selected.ProfileID,
		string(plan.Profile.Canonical),
		selected.Digest,
		len(plan.Members),
		plan.SeatMask,
		plan.Now,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("netplay/create session: %w", err)
	}
	return nil
}

func (records sessionStartRecords) participant(
	ctx context.Context,
	plan netplay.SessionStartPlan,
	member netplay.SeatMember,
) error {
	_, err := recordstore.CreateNetplaySessionParticipants(ctx, records.executor, `
INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,
state,credential_generation,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'LOCKED',0,1,?,?)
`, plan.SessionID, member.ProfileID, member.ID, member.PlayerNo, plan.Now, plan.Now)
	if err != nil {
		return fmt.Errorf("netplay/create session participant: %w", err)
	}
	return nil
}
