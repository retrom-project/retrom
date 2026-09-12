package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/netplay"
)

func requireRoomChange(result sql.Result, err error, conflict error) error {
	if err != nil {
		return fmt.Errorf("netplay/write room control: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("netplay/room change count: %w", err)
	}
	if count != 1 {
		return conflict
	}
	return nil
}

func (records roomControlRecords) Select(ctx context.Context, plan netplay.RoomSelectionPlan) error {
	selected, evidence := plan.Selection, plan.Evidence
	result, err := recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
		Set: `state='WAITING',selected_game_id=?,selected_game_variant_id=?,netplay_profile_id=?,profile_digest=?,
max_players=?,current_session_id=NULL,version=version+1,expires_at_ms=?,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=?`, Args: []any{plan.Before.RoomID, plan.Before.Version}},
		Values: []any{
			selected.GameID,
			selected.VariantID,
			selected.ProfileID,
			selected.Digest,
			selected.MaxPlayers,
			evidence.ExpiresAtMS,
			evidence.Now,
		},
	})
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return err
	}
	if err := records.clearReady(ctx, plan.Before.RoomID, evidence.Now); err != nil {
		return err
	}
	return records.event(ctx, plan.Before.RoomID, evidence)
}

func (records roomControlRecords) Clear(ctx context.Context, plan netplay.RoomClearPlan) error {
	result, err := recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
		Set: `state='DRAFT',selected_game_id=NULL,selected_game_variant_id=NULL,netplay_profile_id=NULL,
profile_digest=NULL,max_players=NULL,current_session_id=NULL,version=version+1,expires_at_ms=?,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND version=?`, Args: []any{plan.Before.RoomID, plan.Before.Version}},
		Values: []any{plan.Evidence.ExpiresAtMS, plan.Evidence.Now},
	})
	if err := requireRoomChange(result, err, netplay.ErrPrecondition); err != nil {
		return err
	}
	if err := records.clearReady(ctx, plan.Before.RoomID, plan.Evidence.Now); err != nil {
		return err
	}
	return records.event(ctx, plan.Before.RoomID, plan.Evidence)
}

func (records roomControlRecords) clearReady(ctx context.Context, roomID string, now int64) error {
	_, err := recordstore.UpdateNetplayRoomMembers(ctx, records.executor, recordstore.Update{
		Set:   `ready=0,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `room_id=? AND left_at_ms IS NULL`, Args: []any{roomID}}, Values: []any{now},
	})
	if err != nil {
		return fmt.Errorf("netplay/clear member readiness: %w", err)
	}
	return nil
}

func (records roomControlRecords) event(
	ctx context.Context,
	roomID string,
	evidence netplay.RoomControlEvidence,
) error {
	_, err := records.executor.ExecContext(ctx, `
INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,NULL,?,?,?,?,?)
`, roomID, evidence.ActorID, evidence.PlayerNo, evidence.Type, string(evidence.Data), evidence.Now)
	if err != nil {
		return fmt.Errorf("netplay/room control event: %w", err)
	}
	return nil
}

func (records roomControlRecords) touch(
	ctx context.Context,
	before netplay.RoomControlSnapshot,
	evidence netplay.RoomControlEvidence,
) error {
	result, err := recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
		Set:    `version=version+1,expires_at_ms=?,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND version=?`, Args: []any{before.RoomID, before.Version}},
		Values: []any{evidence.ExpiresAtMS, evidence.Now},
	})
	return requireRoomChange(result, err, netplay.ErrPrecondition)
}
