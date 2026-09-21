package launch

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/launch"
)

func (records playRecords) Start(ctx context.Context, plan application.PlayStart) error {
	if err := records.advanceLaunch(ctx, plan.Source, plan.NowMS, plan.IdleExpiresAtMS, false); err != nil {
		return err
	}
	if err := requirePlayChange(records.transaction.ExecContext(ctx, `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,started_at_ms,last_heartbeat_at_ms,
active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,0,0,'ACTIVE',1,?,?)`, plan.PlayID, plan.Source.Ref.ID, plan.Source.ProfileID, plan.Source.GameID,
		plan.NowMS, plan.NowMS, plan.NowMS, plan.NowMS)); err != nil {
		return err
	}
	return records.insertEvent(ctx, plan.PlayID, "START", plan.Event, 0, plan.NowMS)
}

func (records playRecords) Progress(ctx context.Context, plan application.PlayProgress) error {
	state, kind := "ACTIVE", "HEARTBEAT"
	var endedAt *int64
	if plan.Kind == "finish" {
		state, kind, endedAt = "FINISHED", "FINISH", &plan.NowMS
	}
	if err := requirePlayChange(
		records.transaction.ExecContext(ctx, `
UPDATE play_sessions SET last_heartbeat_at_ms=?,ended_at_ms=?,active_duration_ms=active_duration_ms+?,
last_client_sequence=?,state=?,version=version+1,updated_at_ms=?
WHERE id=? AND launch_session_id=? AND version=? AND last_client_sequence=? AND state='ACTIVE'
 AND active_duration_ms=?`, plan.NowMS, endedAt, plan.AcceptedDurationMS, plan.Event.ClientSequence, state, plan.NowMS,
			plan.Current.ID, plan.Source.Ref.ID, plan.Current.Version, plan.Current.LastSequence, plan.Current.ActiveDurationMS),
	); err != nil {
		return err
	}
	if err := records.insertEvent(
		ctx,
		plan.Current.ID,
		kind,
		plan.Event,
		plan.AcceptedDurationMS,
		plan.NowMS,
	); err != nil {
		return err
	}
	return records.advanceLaunch(ctx, plan.Source, plan.NowMS, plan.IdleExpiresAtMS, plan.Kind == "finish")
}

func (records playRecords) insertEvent(
	ctx context.Context,
	id, kind string,
	event application.PlayEvent,
	accepted, now int64,
) error {
	interval := application.Interval{}
	if event.PreviousInterval != nil {
		interval = *event.PreviousInterval
	}
	return requirePlayChange(records.transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,client_sequence,event_kind,client_observed_at_ms,
server_received_at_ms,running,visible,paused,accepted_duration_ms,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?)`, id, event.ClientSequence, kind, event.ClientObservedAtMS, now,
		interval.Running, interval.Visible, interval.Paused, accepted, now))
}

func (records playRecords) advanceLaunch(
	ctx context.Context,
	source application.PlaySource,
	now, idleEnd int64,
	finish bool,
) error {
	if source.Ref.Preview {
		return application.ErrBlocked
	}
	change := recordstore.Update{
		Set: `idle_expires_at_ms=?,updated_at_ms=?,version=version+1`, Values: []any{idleEnd, now},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='ACTIVE' AND hard_expires_at_ms>?
 AND (idle_expires_at_ms IS NULL OR idle_expires_at_ms>?)`, Args: []any{source.Ref.ID, source.Version, now, now},
		},
	}
	if finish {
		change.Set = `state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1`
		change.Values = []any{now, now}
	}
	if err := requirePlayChange(sessionstore.ChangeLaunch(ctx, records.transaction, change)); err != nil {
		return fmt.Errorf("advance launch for play: %w", err)
	}
	return nil
}

func (records playRecords) Finish(ctx context.Context, plan application.PlayFinish) error {
	change := recordstore.Update{
		Set: `state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1`, Values: []any{plan.NowMS, plan.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=? AND state IN ('CREATED','ACTIVE') AND hard_expires_at_ms>?`,
			Args:  []any{plan.Source.Ref.ID, plan.Source.Version, plan.Source.Session.State, plan.NowMS},
		},
	}
	if plan.Source.Ref.Preview {
		return requirePlayChange(sessionstore.ChangePreview(ctx, records.transaction, change))
	}
	return requirePlayChange(sessionstore.ChangeLaunch(ctx, records.transaction, change))
}
