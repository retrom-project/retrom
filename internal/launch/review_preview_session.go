package launch

import (
	"context"
	"database/sql"
	"fmt"
)

// Review trials consume the ordinary Player events, but do not create product
// play statistics or a second proof workflow. Closing during loading is valid.
func recordReviewPreviewPlay(
	ctx context.Context,
	transaction *sql.Tx,
	previewID, state, kind string,
	event PlayEvent,
	now int64,
) (PlayResult, error) {
	if !validReviewPlayEvent(kind, event) {
		return PlayResult{}, ErrBlocked
	}
	if state == "FINISHED" && kind == "finish" {
		return reviewPlayResult(event.ClientSequence, state), nil
	}
	if state != "ACTIVE" && (state != "CREATED" || kind != "finish") {
		return PlayResult{}, ErrBlocked
	}
	if kind == "finish" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE review_preview_sessions
SET state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND state IN ('CREATED','ACTIVE')
`, now, now, previewID); err != nil {
			return PlayResult{}, fmt.Errorf("finish review trial: %w", err)
		}
		state = "FINISHED"
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("record review trial event: %w", err)
	}
	return reviewPlayResult(event.ClientSequence, state), nil
}

func validReviewPlayEvent(kind string, event PlayEvent) bool {
	if kind == "start" {
		return event.ClientSequence == 0 && event.PreviousInterval == nil
	}
	if kind != "heartbeat" && kind != "finish" {
		return false
	}
	return event.ClientSequence > 0 && event.PreviousInterval != nil ||
		kind == "finish" && event.ClientSequence == 0 && event.PreviousInterval == nil
}

func reviewPlayResult(sequence int64, state string) PlayResult {
	return PlayResult{PlaySessionID: nil, ClientSequence: sequence, AcceptedDuration: 0, State: state}
}
