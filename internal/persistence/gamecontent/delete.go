package gamecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/auditevents"
	payloadpersistence "retrom/internal/persistence/payloadrelease"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/gamecontent"
	payloadservice "retrom/internal/service/payloadrelease"

	"github.com/google/uuid"
)

const deleteGameOperation = "deleteAdminGame"

func (writes writes) LoadDeleteGameState(
	ctx context.Context, gameID string,
) (application.DeleteGameState, error) {
	var state application.DeleteGameState
	var releaseJobID sql.NullString
	err := writes.transaction.QueryRowContext(ctx, `
SELECT title,status,payload_state,payload_release_job_id,version
FROM games
WHERE id=?
`, gameID).Scan(
		&state.Title, &state.Status, &state.PayloadState, &releaseJobID, &state.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.DeleteGameState{}, application.ErrDeleteGameNotFound
	}
	if err != nil {
		return application.DeleteGameState{}, fmt.Errorf("read game deletion state: %w", err)
	}
	if releaseJobID.Valid {
		state.PayloadReleaseJobID = &releaseJobID.String
	}
	return state, nil
}

func (writes writes) LoadDeleteGameReplay(
	ctx context.Context, principalID, key string, now int64,
) (application.DeleteGameReplay, bool, error) {
	if _, err := writes.transaction.ExecContext(ctx, `
DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
`, principalID, deleteGameOperation, key, now); err != nil {
		return application.DeleteGameReplay{}, false, fmt.Errorf("expire game deletion replay: %w", err)
	}
	var replay application.DeleteGameReplay
	err := writes.transaction.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body
FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=?
`, principalID, deleteGameOperation, key).Scan(
		&replay.RequestDigest, &replay.HTTPStatus, &replay.HeadersJSON, &replay.Body,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.DeleteGameReplay{}, false, nil
	}
	if err != nil {
		return application.DeleteGameReplay{}, false, fmt.Errorf("read game deletion replay: %w", err)
	}
	return replay, true, nil
}

func (writes writes) DeleteGameImpact(
	ctx context.Context, gameID string,
) (application.DeleteGameImpact, error) {
	impact, err := payloadservice.NewImpactQueries(
		payloadpersistence.BindImpact(writes.transaction),
	).Game(ctx, gameID)
	if err != nil {
		return application.DeleteGameImpact{}, fmt.Errorf("read game deletion impact: %w", err)
	}
	return application.DeleteGameImpact{
		ImpactDigest:      impact.ImpactDigest,
		RegisteredBytes:   impact.RegisteredBytes,
		ExclusiveBytes:    impact.ExclusiveBytes,
		SharedBytes:       impact.SharedBytes,
		BlobCount:         impact.BlobCount,
		SaveStateCount:    impact.SaveStateCount,
		AssetCount:        impact.AssetCount,
		ContentFileCount:  impact.ContentFileCount,
		ActiveLaunchCount: impact.ActiveLaunchCount,
		SourceKinds:       impact.SourceKinds,
	}, nil
}

func (writes writes) ScheduleGameDeletion(
	ctx context.Context, gameID string, version, now int64,
) (string, error) {
	jobID, err := payloadservice.NewScheduler(nil).DeleteGame(
		ctx, payloadpersistence.BindScheduling(writes.transaction), gameID, version, now,
	)
	if err != nil {
		return "", fmt.Errorf("schedule game payload release: %w", err)
	}
	return jobID, nil
}

func (writes writes) TransitionDeletedGameRuntime(
	ctx context.Context, gameID string, now int64,
) error {
	if _, err := writes.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,CASE state WHEN 'QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
'{"schemaVersion":1,"reason":"GAME_DELETED"}',?
FROM jobs WHERE scope_type='GAME' AND scope_id=?
AND kind IN ('GAME_CONTENT_REPLACE','METADATA_SCRAPE','MEDIA_FETCH') AND state IN ('QUEUED','RUNNING')
`, now, gameID); err != nil {
		return fmt.Errorf("insert game deletion job events: %w", err)
	}
	if _, err := writes.transaction.ExecContext(ctx, `
UPDATE jobs SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
cancel_requested_at_ms=?,cancel_reason='game deleted',finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE NULL END,
version=version+1,updated_at_ms=? WHERE scope_type='GAME' AND scope_id=?
AND kind IN ('GAME_CONTENT_REPLACE','METADATA_SCRAPE','MEDIA_FETCH') AND state IN ('QUEUED','RUNNING')
`, now, now, now, gameID); err != nil {
		return fmt.Errorf("transition game deletion jobs: %w", err)
	}
	if _, err := sessionstore.ChangeLaunch(ctx, writes.transaction, recordstore.Update{
		Set: `
state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,
version=version+1
`,
		Scope: recordstore.Scope{
			Where: `game_id=? AND state IN ('CREATED','ACTIVE')`, Args: []any{gameID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("revoke game deletion launches: %w", err)
	}
	if _, err := writes.transaction.ExecContext(ctx, `
UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE game_id=? AND state='ACTIVE'`, now, now, gameID); err != nil {
		return fmt.Errorf("abandon game deletion sessions: %w", err)
	}
	if _, err := sessionstore.ChangeLaunch(ctx, writes.transaction, recordstore.Update{
		Set: `save_state_id=NULL`,
		Scope: recordstore.Scope{
			Where: `game_id=? AND save_state_id IS NOT NULL`, Args: []any{gameID},
		},
	}); err != nil {
		return fmt.Errorf("clear game deletion saves: %w", err)
	}
	return nil
}

func (writes writes) RecordDeleteGameAudit(
	ctx context.Context, event application.DeleteGameAudit,
) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create game deletion audit identity: %w", err)
	}
	actor := event.Actor
	if err := auditevents.Insert(ctx, writes.transaction, auditevents.Event{
		ID: id.String(), ActorKind: actor.Kind, ActorUserID: actor.UserID, ActorLabel: actor.Label,
		Action: "GAME_PERMANENT_DELETE_REQUESTED", ResourceType: "GAME", ResourceID: event.GameID,
		Before: event.Before, After: event.After, RequestID: actor.RequestID, CreatedAtMS: event.NowMS,
	}); err != nil {
		return fmt.Errorf("insert game deletion audit event: %w", err)
	}
	return nil
}

func (writes writes) StoreDeleteGameReplay(
	ctx context.Context, replay application.DeleteGameReplayWrite,
) error {
	if _, err := writes.transaction.ExecContext(ctx, `
INSERT INTO idempotency_records(
principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
)
VALUES(?,?,?,?,?,?,?,?,?)
`, replay.PrincipalID, deleteGameOperation, replay.Key, replay.RequestDigest, replay.HTTPStatus,
		replay.HeadersJSON, replay.Body, replay.CreatedAtMS, replay.ExpiresAtMS); err != nil {
		return fmt.Errorf("write game deletion replay: %w", err)
	}
	return nil
}

var (
	_ application.DeleteGameReader = writes{}
	_ application.DeleteGameWriter = writes{}
)
