package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"
)

type Play struct{ database *sql.DB }

func NewPlay(database *sql.DB) *Play { return &Play{database: database} }
func (repository *Play) WithPlay(ctx context.Context, work func(application.PlayScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin play transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := playRecords{transaction: tx}
	if err := work(application.PlayScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit play transaction: %w", err)
	}
	return nil
}

type playRecords struct{ transaction *sql.Tx }

func (records playRecords) Source(ctx context.Context, id string) (application.PlaySource, bool, error) {
	var source application.PlaySource
	var idle sql.NullInt64
	err := records.transaction.QueryRowContext(ctx, `
SELECT id,0,credential_sha256,state,profile_id,game_id,hard_expires_at_ms,idle_expires_at_ms,version
FROM launch_sessions WHERE id=?
UNION ALL
SELECT id,1,credential_sha256,state,'','',hard_expires_at_ms,NULL,version
FROM review_preview_sessions WHERE id=?`, id, id).Scan(
		&source.Ref.ID, &source.Ref.Preview, &source.Session.CredentialHash, &source.Session.State,
		&source.ProfileID, &source.GameID, &source.Session.HardExpiresAtMS, &idle, &source.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PlaySource{}, false, nil
	}
	if err != nil {
		return application.PlaySource{}, false, fmt.Errorf("query play source: %w", err)
	}
	if idle.Valid {
		source.IdleExpiresAtMS = &idle.Int64
	}
	return source, true, nil
}

func (records playRecords) Current(ctx context.Context, id string) (application.PlayRecord, bool, error) {
	var result application.PlayRecord
	err := records.transaction.QueryRowContext(ctx, `
SELECT id,state,version,last_client_sequence,last_heartbeat_at_ms,active_duration_ms
FROM play_sessions WHERE launch_session_id=?`, id).Scan(&result.ID, &result.State, &result.Version,
		&result.LastSequence, &result.LastHeartbeatAtMS, &result.ActiveDurationMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PlayRecord{}, false, nil
	}
	if err != nil {
		return application.PlayRecord{}, false, fmt.Errorf("query current play: %w", err)
	}
	return result, true, nil
}

func (records playRecords) Event(
	ctx context.Context,
	id string,
	sequence int64,
) (application.StoredPlayEvent, bool, error) {
	var result application.StoredPlayEvent
	err := records.transaction.QueryRowContext(ctx, `
SELECT event_kind,client_observed_at_ms,accepted_duration_ms,running,visible,paused
FROM play_session_events WHERE play_session_id=? AND client_sequence=?`, id, sequence).
		Scan(&result.Kind, &result.ClientObservedAtMS, &result.AcceptedDurationMS,
			&result.Interval.Running, &result.Interval.Visible, &result.Interval.Paused)
	if errors.Is(err, sql.ErrNoRows) {
		return application.StoredPlayEvent{}, false, nil
	}
	if err != nil {
		return application.StoredPlayEvent{}, false, fmt.Errorf("query prior play event: %w", err)
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
