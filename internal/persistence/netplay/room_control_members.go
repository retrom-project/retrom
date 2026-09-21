package netplay

import (
	"context"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/netplay"
)

func (records roomControlRecords) Seat(ctx context.Context, plan netplay.RoomSeatPlan) error {
	if err := records.touch(ctx, plan.Before, plan.Evidence); err != nil {
		return err
	}
	if err := records.seatMember(ctx, plan); err != nil {
		return err
	}
	return records.event(ctx, plan.Before.RoomID, plan.Evidence)
}

func (records roomControlRecords) seatMember(ctx context.Context, plan netplay.RoomSeatPlan) error {
	if plan.Before.Member != nil {
		result, err := recordstore.UpdateNetplayRoomMembers(ctx, records.executor, recordstore.Update{
			Set: `player_no=?,ready=0,left_at_ms=NULL,leave_reason=NULL,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=? AND room_id=? AND profile_id=? AND version=?`,
				Args:  []any{plan.MemberID, plan.Before.RoomID, plan.Evidence.ActorID, plan.Before.Member.Version},
			},
			Values: []any{plan.PlayerNo, plan.Evidence.Now},
		})
		return requireRoomChange(result, err, netplay.ErrPrecondition)
	}
	result, err := recordstore.CreateNetplayRoomMembers(ctx, records.executor, `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'GUEST',?,0,1,?,?)
`, plan.MemberID, plan.Before.RoomID, plan.Evidence.ActorID, plan.PlayerNo, plan.Evidence.Now, plan.Evidence.Now)
	return requireRoomChange(result, err, netplay.ErrPrecondition)
}

func (records roomControlRecords) Ready(ctx context.Context, plan netplay.RoomReadyPlan) error {
	if err := records.touch(ctx, plan.Before, plan.Evidence); err != nil {
		return err
	}
	member := plan.Before.Member
	result, err := recordstore.UpdateNetplayRoomMembers(ctx, records.executor, recordstore.Update{
		Set: `ready=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND room_id=? AND profile_id=? AND version=? AND left_at_ms IS NULL`,
			Args:  []any{member.ID, plan.Before.RoomID, plan.Evidence.ActorID, member.Version},
		},
		Values: []any{plan.Ready, plan.Evidence.Now},
	})
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return err
	}
	return records.event(ctx, plan.Before.RoomID, plan.Evidence)
}
