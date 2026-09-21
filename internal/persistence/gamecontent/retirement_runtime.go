package gamecontent

import (
	"context"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	"retrom/internal/service/gamecontent"
)

func (records retirementRecords) Change(ctx context.Context, value gamecontent.RetirementChange) error {
	before := value.Before
	scope := recordstore.Scope{
		Where: `id=? AND game_id=? AND version=? AND state=?`,
		Args:  []any{before.ID, value.GameID, before.Version, before.State},
	}
	switch before.Kind {
	case gamecontent.RetirementLaunch:
		return records.launch(ctx, value)
	case gamecontent.RetirementPlay:
		return requireChanged(records.executor.ExecContext(ctx, `UPDATE play_sessions
SET state=?,ended_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=? AND game_id=? AND version=? AND state=?`,
			value.State, value.Now, value.Now, before.ID, value.GameID, before.Version, before.State))
	case gamecontent.RetirementNetplay:
		return requireChanged(recordstore.UpdateNetplaySessions(ctx, records.executor, recordstore.Update{
			Set: `state=?,finished_at_ms=?,end_reason=?,updated_at_ms=?,version=version+1`, Scope: scope,
			Values: []any{value.State, value.Now, value.Reason, value.Now},
		}))
	case gamecontent.RetirementRoom:
		scope.Where = `id=? AND selected_game_id=? AND version=? AND state=?`
		return requireChanged(recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
			Set: `state=?,current_session_id=NULL,ended_at_ms=?,end_reason=?,updated_at_ms=?,version=version+1`, Scope: scope,
			Values: []any{value.State, value.Now, value.Reason, value.Now},
		}))
	case gamecontent.RetirementVariant:
		scope.Where = `id=? AND game_id=? AND version=? AND status=?`
		return requireChanged(recordstore.UpdateGameVariants(ctx, records.executor, recordstore.Update{
			Set:    `status=?,compatibility_code='CONTENT_REPLACED',emulator_game_id=NULL,version=version+1,updated_at_ms=?`,
			Scope:  scope,
			Values: []any{value.State, value.Now},
		}))
	default:
		return gamecontent.ErrInvalid
	}
}

func (records retirementRecords) launch(ctx context.Context, value gamecontent.RetirementChange) error {
	before := value.Before
	update := recordstore.Update{
		Set:    `state=?,save_state_id=NULL,finished_at_ms=?,updated_at_ms=?,version=version+1`,
		Values: []any{value.State, value.FinishedAt, value.Now},
		Scope: recordstore.Scope{
			Where: `id=? AND game_id=? AND version=? AND state=?
AND ((save_state_id IS NULL AND ? IS NULL) OR save_state_id=?)
AND ((finished_at_ms IS NULL AND ? IS NULL) OR finished_at_ms=?)`,
			Args: []any{
				before.ID, value.GameID, before.Version, before.State,
				before.SaveID, before.SaveID, before.FinishedAt, before.FinishedAt,
			},
		},
	}
	return requireChanged(sessionstore.ChangeLaunch(ctx, records.executor, update))
}
