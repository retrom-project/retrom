package launch

import (
	"context"

	application "retrom/internal/service/launch"
)

func (records playRecords) Snapshot(ctx context.Context, plan application.PlaySnapshotPlan) error {
	var err error
	delta := plan.ActiveDurationMS
	newSession := int64(1)
	if plan.Current == nil {
		err = requirePlayChange(records.transaction.ExecContext(ctx, `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,started_at_ms,last_reported_at_ms,
active_duration_ms,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,'ACTIVE',1,?,?)`, plan.PlayID, plan.Source.LaunchID, plan.Source.ProfileID,
			plan.Source.GameID, plan.NowMS, plan.NowMS, plan.ActiveDurationMS, plan.NowMS, plan.NowMS))
	} else {
		delta -= plan.Current.ActiveDurationMS
		newSession = 0
		err = requirePlayChange(records.transaction.ExecContext(ctx, `
UPDATE play_sessions SET last_reported_at_ms=?,active_duration_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND launch_session_id=? AND version=? AND state='ACTIVE' AND active_duration_ms<=?`,
			plan.NowMS, plan.ActiveDurationMS, plan.NowMS, plan.PlayID, plan.Source.LaunchID,
			plan.Current.Version, plan.ActiveDurationMS))
	}
	if err != nil {
		return err
	}
	return requirePlayChange(records.transaction.ExecContext(ctx, `
INSERT INTO profile_game_activity(profile_id,game_id,last_played_at_ms,active_duration_ms,session_count)
SELECT profile_id,game_id,started_at_ms,active_duration_ms,1 FROM play_sessions WHERE id=?
ON CONFLICT(profile_id,game_id) DO UPDATE SET
last_played_at_ms=max(profile_game_activity.last_played_at_ms,excluded.last_played_at_ms),
active_duration_ms=profile_game_activity.active_duration_ms+?,
session_count=profile_game_activity.session_count+?`, plan.PlayID, delta, newSession))
}
