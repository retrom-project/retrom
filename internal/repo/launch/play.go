package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

type Play struct {
	database *sql.DB
}

func NewPlay(database *sql.DB) *Play { return &Play{database: database} }

func (repository *Play) LoadPlaySource(
	ctx context.Context, id string,
) (application.PlaySource, bool, error) {
	return playRecords{executor: repository.database}.Source(ctx, id)
}

func (repository *Play) LoadPlayRecord(
	ctx context.Context, id string,
) (application.PlayRecord, bool, error) {
	return playRecords{executor: repository.database}.Current(ctx, id)
}

func (repository *Play) LoadPlayEvent(
	ctx context.Context, id string, sequence int64,
) (application.StoredPlayEvent, bool, error) {
	return playRecords{executor: repository.database}.Event(ctx, id, sequence)
}

func (repository *Play) commitPlay(
	ctx context.Context, _ string,
	execute func(playRecords) error,
) error {
	return dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		return execute(playRecords{executor: db})
	})
}

func (repository *Play) CommitPlayStart(
	ctx context.Context, plan application.PlayStart,
) error {
	return repository.commitPlay(ctx, "start", func(records playRecords) error {
		return records.Start(ctx, plan)
	})
}

func (repository *Play) CommitPlayProgress(
	ctx context.Context, plan application.PlayProgress,
) error {
	return repository.commitPlay(ctx, "progress", func(records playRecords) error {
		return records.Progress(ctx, plan)
	})
}

func (repository *Play) CommitPlayFinish(
	ctx context.Context, plan application.PlayFinish,
) error {
	return repository.commitPlay(ctx, "finish", func(records playRecords) error {
		return records.Finish(ctx, plan)
	})
}

type playRecords struct {
	executor dbexec.Executor
}

func (records playRecords) Source(
	ctx context.Context, id string,
) (application.PlaySource, bool, error) {
	var source application.PlaySource
	var idle sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `
SELECT id,0,credential_sha256,state,profile_id,game_id,hard_expires_at_ms,idle_expires_at_ms,version
FROM launch_sessions WHERE id=?
UNION ALL
SELECT id,1,credential_sha256,state,'','',hard_expires_at_ms,NULL,version
FROM review_preview_sessions WHERE id=?`, id, id).Scan(
		&source.Ref.ID, &source.Ref.Preview,
		&source.Session.CredentialHash, &source.Session.State,
		&source.ProfileID, &source.GameID,
		&source.Session.HardExpiresAtMS, &idle, &source.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PlaySource{}, false, nil
	}
	if err != nil {
		return application.PlaySource{}, false, fmt.Errorf(
			"query play source: %w", err,
		)
	}
	if idle.Valid {
		source.IdleExpiresAtMS = &idle.Int64
	}
	return source, true, nil
}

func (records playRecords) Current(
	ctx context.Context, id string,
) (application.PlayRecord, bool, error) {
	var result application.PlayRecord
	err := records.executor.QueryRowContext(ctx, `
SELECT id,state,version,last_client_sequence,last_heartbeat_at_ms,active_duration_ms
FROM play_sessions WHERE launch_session_id=?`, id).Scan(
		&result.ID, &result.State, &result.Version,
		&result.LastSequence, &result.LastHeartbeatAtMS,
		&result.ActiveDurationMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PlayRecord{}, false, nil
	}
	if err != nil {
		return application.PlayRecord{}, false, fmt.Errorf(
			"query current play: %w", err,
		)
	}
	return result, true, nil
}

func (records playRecords) Event(
	ctx context.Context, id string, sequence int64,
) (application.StoredPlayEvent, bool, error) {
	var result application.StoredPlayEvent
	err := records.executor.QueryRowContext(ctx, `
SELECT event_kind,client_observed_at_ms,accepted_duration_ms,running,visible,paused
FROM play_session_events WHERE play_session_id=? AND client_sequence=?`,
		id, sequence).Scan(
		&result.Kind, &result.ClientObservedAtMS,
		&result.AcceptedDurationMS,
		&result.Interval.Running, &result.Interval.Visible,
		&result.Interval.Paused,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.StoredPlayEvent{}, false, nil
	}
	if err != nil {
		return application.StoredPlayEvent{}, false, fmt.Errorf(
			"query prior play event: %w", err,
		)
	}
	return result, true, nil
}

func requirePlayChange(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write play lifecycle: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read play change count: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("play lifecycle conflict: %w", application.ErrBlocked)
	}
	return nil
}
