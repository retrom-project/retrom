package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) RecordPlay(
	ctx context.Context,
	launchID, capability, kind string,
	event PlayEvent,
) (PlayResult, error) {
	if event.ClientObservedAtMS < 0 || event.ClientObservedAtMS > 253402300799999 {
		return PlayResult{}, ErrBlocked
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	source, err := loadPlayLaunch(ctx, transaction, launchID, capability, now)
	if err != nil {
		return PlayResult{}, err
	}
	if source.purpose == "RPG_RUNTIME_VALIDATION" {
		return recordRPGValidationPlay(ctx, transaction, launchID, source.state, kind, event, now)
	}
	return recordProductPlay(ctx, transaction, launchID, kind, event, now, source)
}

type playLaunchSource struct {
	purpose, state, profileID string
	gameID, variantRevisionID sql.NullString
	idleExpires               sql.NullInt64
}

func loadPlayLaunch(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, capability string,
	now int64,
) (playLaunchSource, error) {
	var source playLaunchSource
	var credentialHash []byte
	var hardExpires int64
	if err := transaction.QueryRowContext(ctx, `
SELECT credential_sha256,
purpose,
state,
profile_id,
game_id,
game_variant_revision_id,
hard_expires_at_ms,
idle_expires_at_ms
FROM launch_sessions
WHERE id=?
`, launchID).Scan(
		&credentialHash,
		&source.purpose,
		&source.state,
		&source.profileID,
		&source.gameID,
		&source.variantRevisionID,
		&hardExpires,
		&source.idleExpires,
	); err != nil ||
		!retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= now {
		return playLaunchSource{}, ErrCredential
	}
	return source, nil
}

func recordProductPlay(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, kind string,
	event PlayEvent,
	now int64,
	source playLaunchSource,
) (PlayResult, error) {
	if source.purpose != "PRODUCT" || !source.gameID.Valid || !source.variantRevisionID.Valid {
		return PlayResult{}, ErrBlocked
	}
	var playID, playState string
	var lastSequence, lastHeartbeat int64
	err := transaction.QueryRowContext(ctx, `
SELECT id,
state,
last_client_sequence,
last_heartbeat_at_ms
FROM play_sessions
WHERE launch_session_id=?
`, launchID).
		Scan(&playID, &playState, &lastSequence, &lastHeartbeat)
	if err == nil && event.ClientSequence <= lastSequence {
		return replayPlayEvent(ctx, transaction, playID, kind, event)
	}
	if kind == "start" {
		return startPlaySession(
			ctx, transaction, launchID, source.profileID, source.gameID.String, source.variantRevisionID.String,
			source.state, playID, playState, err, event, now,
		)
	}
	if errors.Is(err, sql.ErrNoRows) && kind == "finish" && event.ClientSequence == 0 &&
		event.PreviousInterval == nil {
		return finishLaunchWithoutPlay(ctx, transaction, launchID, now)
	}
	if !validPlayProgress(
		err, playState, source.state, source.idleExpires, event, lastSequence, kind, now,
	) {
		return PlayResult{}, ErrBlocked
	}
	return recordPlayProgress(
		ctx, transaction, launchID, playID, kind, event, lastHeartbeat, now,
	)
}

// RPG runtime-validation launches deliberately do not create play_sessions:
// they are not attached to a published game and therefore have no game_id to
// account play time against. Config activation owns CREATED -> ACTIVE; the play
// endpoint only accepts the normal player event shapes and closes the launch on
// finish so a checkpointed validation can create its distinct restore launch.
func recordRPGValidationPlay(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, launchState, kind string,
	event PlayEvent,
	now int64,
) (PlayResult, error) {
	if !validRPGPlayEvent(kind, event) {
		return PlayResult{}, ErrBlocked
	}
	if launchState == "FINISHED" && kind == "finish" {
		return rpgPlayResult(event.ClientSequence, "FINISHED"), nil
	}
	if launchState != "ACTIVE" {
		return PlayResult{}, ErrBlocked
	}
	state := "ACTIVE"
	if kind == "finish" {
		if err := finishRPGValidationLaunch(ctx, transaction, launchID, now); err != nil {
			return PlayResult{}, err
		}
		state = "FINISHED"
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	return rpgPlayResult(event.ClientSequence, state), nil
}

func validRPGPlayEvent(kind string, event PlayEvent) bool {
	if kind == "start" {
		return event.ClientSequence == 0 && event.PreviousInterval == nil
	}
	if kind != "heartbeat" && kind != "finish" {
		return false
	}
	return event.ClientSequence > 0 && event.PreviousInterval != nil ||
		kind == "finish" && event.ClientSequence == 0 && event.PreviousInterval == nil
}

func finishRPGValidationLaunch(
	ctx context.Context,
	transaction *sql.Tx,
	launchID string,
	now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND purpose='RPG_RUNTIME_VALIDATION' AND state='ACTIVE'
`, now, now, launchID)
	if err != nil {
		return fmt.Errorf("launch/service: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return ErrBlocked
	}
	return nil
}

func rpgPlayResult(sequence int64, state string) PlayResult {
	return PlayResult{
		PlaySessionID: nil, ClientSequence: sequence,
		AcceptedDuration: 0, State: state,
	}
}

func replayPlayEvent(
	ctx context.Context,
	transaction *sql.Tx,
	playID, kind string,
	event PlayEvent,
) (PlayResult, error) {
	var storedKind string
	var storedObserved, accepted int64
	var running, visible, paused bool
	if err := transaction.QueryRowContext(ctx, `
SELECT event_kind,
client_observed_at_ms,
running,
visible,
paused,
accepted_duration_ms
FROM play_session_events
WHERE play_session_id=?
AND client_sequence=?
`, playID, event.ClientSequence).Scan(
		&storedKind, &storedObserved, &running, &visible, &paused, &accepted,
	); err != nil {
		return PlayResult{}, ErrBlocked
	}
	expectedKind := map[string]string{"start": "START", "finish": "FINISH"}[kind]
	if expectedKind == "" {
		expectedKind = "HEARTBEAT"
	}
	intervalMatches := event.PreviousInterval == nil && storedKind == "START" ||
		event.PreviousInterval != nil && running == event.PreviousInterval.Running &&
			visible == event.PreviousInterval.Visible && paused == event.PreviousInterval.Paused
	if storedKind != expectedKind || storedObserved != event.ClientObservedAtMS || !intervalMatches {
		return PlayResult{}, ErrBlocked
	}
	state := "ACTIVE"
	if storedKind == "FINISH" {
		state = "FINISHED"
	}
	return PlayResult{
		PlaySessionID: playID, ClientSequence: event.ClientSequence,
		AcceptedDuration: accepted, State: state,
	}, nil
}

func startPlaySession(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, profileID, gameID, variantRevisionID, launchState, playID, playState string,
	lookupErr error,
	event PlayEvent,
	now int64,
) (PlayResult, error) {
	if event.ClientSequence != 0 || event.PreviousInterval != nil || launchState != "ACTIVE" {
		return PlayResult{}, ErrBlocked
	}
	if lookupErr == nil {
		return PlayResult{PlaySessionID: playID, ClientSequence: 0, AcceptedDuration: 0, State: playState}, nil
	}
	if !errors.Is(lookupErr, sql.ErrNoRows) {
		return PlayResult{}, fmt.Errorf("launch/service: %w", lookupErr)
	}
	generated, _ := uuid.NewV7()
	playID = generated.String()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_sessions(id,
launch_session_id,
profile_id,
game_id,
game_variant_revision_id,
started_at_ms,
last_heartbeat_at_ms,
active_duration_ms,
last_client_sequence,
state,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
0,
0,
'ACTIVE',
1,
?,
?)
`, playID, launchID, profileID, gameID, variantRevisionID, now, now, now, now); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,
client_sequence,
event_kind,
client_observed_at_ms,
server_received_at_ms,
running,
visible,
paused,
accepted_duration_ms,
created_at_ms) VALUES(?,
0,
'START',
?,
?,
0,
0,
0,
0,
?)
`, playID, event.ClientObservedAtMS, now, now); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET idle_expires_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now+int64(2*time.Minute/time.Millisecond), now, launchID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	return PlayResult{PlaySessionID: playID, ClientSequence: 0, AcceptedDuration: 0, State: "ACTIVE"}, nil
}

func finishLaunchWithoutPlay(
	ctx context.Context,
	transaction *sql.Tx,
	launchID string,
	now int64,
) (PlayResult, error) {
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
AND state='ACTIVE'
`, now, now, launchID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	return PlayResult{PlaySessionID: nil, ClientSequence: 0, AcceptedDuration: 0, State: "FINISHED"}, nil
}

func validPlayProgress(
	lookupErr error,
	playState, launchState string,
	idleExpires sql.NullInt64,
	event PlayEvent,
	lastSequence int64,
	kind string,
	now int64,
) bool {
	return lookupErr == nil && playState == "ACTIVE" && launchState == "ACTIVE" &&
		(!idleExpires.Valid || idleExpires.Int64 > now) && event.PreviousInterval != nil &&
		event.ClientSequence == lastSequence+1 && (kind == "heartbeat" || kind == "finish")
}

func recordPlayProgress(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, playID, kind string,
	event PlayEvent,
	lastHeartbeat, now int64,
) (PlayResult, error) {
	accepted := int64(0)
	if event.PreviousInterval.Running && event.PreviousInterval.Visible && !event.PreviousInterval.Paused {
		accepted = min(now-lastHeartbeat, int64(45*time.Second/time.Millisecond))
		if accepted < 0 {
			accepted = 0
		}
	}
	eventKind := "HEARTBEAT"
	newState := "ACTIVE"
	var endedAt any
	if kind == "finish" {
		eventKind, newState, endedAt = "FINISH", "FINISHED", now
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,
client_sequence,
event_kind,
client_observed_at_ms,
server_received_at_ms,
running,
visible,
paused,
accepted_duration_ms,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		playID,
		event.ClientSequence,
		eventKind,
		event.ClientObservedAtMS,
		now,
		event.PreviousInterval.Running,
		event.PreviousInterval.Visible,
		event.PreviousInterval.Paused,
		accepted,
		now,
	); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions
SET last_heartbeat_at_ms=?,
ended_at_ms=?,
active_duration_ms=active_duration_ms+?,
last_client_sequence=?,
state=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, endedAt, accepted, event.ClientSequence, newState, now, playID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if kind == "finish" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now, now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET idle_expires_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now+int64(2*time.Minute/time.Millisecond), now, launchID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	return PlayResult{
		PlaySessionID:    playID,
		ClientSequence:   event.ClientSequence,
		AcceptedDuration: accepted,
		State:            newState,
	}, nil
}

func MarshalConfig(config Config) ([]byte, error) {
	contents, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal launch config: %w", err)
	}
	return contents, nil
}
