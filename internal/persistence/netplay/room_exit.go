package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/netplay"
)

type RoomExit struct{ database *sql.DB }

func NewRoomExit(database *sql.DB) *RoomExit { return &RoomExit{database} }
func (repository *RoomExit) WithExit(ctx context.Context, work func(netplay.RoomExitScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin room exit: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := roomExitRecords{transaction}
	if err := work(netplay.RoomExitScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("netplay/commit room exit: %w", err)
	}
	return nil
}

type roomExitRecords struct{ executor dbexec.Executor }

func (records roomExitRecords) Current(ctx context.Context, roomID, actorID string) (netplay.RoomExitSnapshot, error) {
	room, err := roomControlRecords(records).Current(ctx, roomID, actorID)
	if err != nil {
		return netplay.RoomExitSnapshot{}, err
	}
	before := netplay.RoomExitSnapshot{Room: room}
	if err := records.executor.QueryRowContext(
		ctx, `SELECT current_session_id FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&before.SessionID); err != nil {
		return netplay.RoomExitSnapshot{}, fmt.Errorf("netplay/read exit session: %w", err)
	}
	return before, nil
}

func (records roomExitRecords) End(ctx context.Context, plan netplay.RoomEndPlan) error {
	result, err := recordstore.UpdateNetplayRooms(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `version=version`,
			Scope: recordstore.Scope{
				Where: `id=? AND version=? AND state=?`,
				Args:  []any{plan.Before.Room.RoomID, plan.Before.Room.Version, plan.Before.Room.State},
			},
		},
	)
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return err
	}
	if plan.Before.SessionID != nil {
		if err := closeNetplaySession(
			ctx,
			records.executor,
			*plan.Before.SessionID,
			plan.SessionState,
			plan.PlayState,
			plan.Reason,
			plan.Now,
		); err != nil {
			return err
		}
	}
	if plan.Disposition == "WAITING" {
		if err := records.waiting(ctx, plan); err != nil {
			return err
		}
	} else {
		if err := endRoomRecord(ctx, records.executor, plan.Before.Room.RoomID, plan.Reason, plan.Now); err != nil {
			return err
		}
	}
	if plan.Disposition == "WAITING" && plan.Before.SessionID == nil {
		return nil
	}
	eventType := "ROOM_ENDED"
	actor := plan.ActorID
	if plan.Disposition == "WAITING" {
		eventType = "SESSION_STATE_CHANGED"
		actor = nil
	}
	_, err = records.executor.ExecContext(
		ctx,
		`
INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,?,?,NULL,?,?,?)`,
		plan.Before.Room.RoomID,
		plan.Before.SessionID,
		actor,
		eventType,
		string(plan.Event),
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("netplay/append exit event: %w", err)
	}
	return nil
}

func (records roomExitRecords) waiting(ctx context.Context, plan netplay.RoomEndPlan) error {
	if plan.LeaveProfileID != "" {
		_, err := recordstore.UpdateNetplayRoomMembers(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `ready=0,left_at_ms=?,leave_reason=?,version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `room_id=? AND profile_id=? AND role='GUEST' AND left_at_ms IS NULL`,
					Args:  []any{plan.Before.Room.RoomID, plan.LeaveProfileID},
				},
				Values: []any{plan.Now, plan.LeaveReason, plan.Now},
			},
		)
		if err != nil {
			return fmt.Errorf("netplay/release departing guest: %w", err)
		}
	}
	if err := roomControlRecords(records).clearReady(ctx, plan.Before.Room.RoomID, plan.Now); err != nil {
		return err
	}
	result, err := recordstore.UpdateNetplayRooms(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `state='WAITING',current_session_id=NULL,version=version+1,expires_at_ms=?,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=? AND version=?`,
				Args:  []any{plan.Before.Room.RoomID, plan.Before.Room.Version},
			},
			Values: []any{plan.ExpiresAtMS, plan.Now},
		},
	)
	return requireRoomChange(result, err, netplay.ErrPrecondition)
}

func (records roomExitRecords) Remove(ctx context.Context, plan netplay.RoomRemovalPlan) error {
	control := roomControlRecords(records)
	if err := control.touch(ctx, plan.Before, plan.Evidence); err != nil {
		return err
	}
	result, err := recordstore.UpdateNetplayRoomMembers(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `ready=0,left_at_ms=?,leave_reason=?,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=? AND room_id=? AND version=? AND left_at_ms IS NULL`,
				Args:  []any{plan.Member.ID, plan.Before.RoomID, plan.Member.Version},
			},
			Values: []any{plan.Evidence.Now, plan.Reason, plan.Evidence.Now},
		},
	)
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return err
	}
	return control.event(ctx, plan.Before.RoomID, plan.Evidence)
}
