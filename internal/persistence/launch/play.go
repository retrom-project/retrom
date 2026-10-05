package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

type Play struct{ database dbapi.DB }

func NewPlay(database dbapi.DB) *Play { return &Play{database: database} }
func (repository *Play) WithPlay(ctx context.Context, work func(application.PlayScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := playRecords{transaction: tx}
		if err := work(application.PlayScope{Read: records, Write: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit play transaction: %w", err)
	}
	return nil
}

type playRecords struct{ transaction dbapi.Tx }

func (records playRecords) Source(ctx context.Context, id string) (application.PlaySource, bool, error) {
	var source application.PlaySource
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT id,credential_sha256,state,profile_id,game_id,hard_expires_at_ms
FROM launch_sessions WHERE id=?`, id).Scan(
		&source.LaunchID, &source.Session.CredentialHash, &source.Session.State,
		&source.ProfileID, &source.GameID, &source.Session.HardExpiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PlaySource{}, false, nil
	}
	if err != nil {
		return application.PlaySource{}, false, fmt.Errorf("query play source: %w", err)
	}
	return source, true, nil
}

func (records playRecords) Current(ctx context.Context, id string) (application.PlayRecord, bool, error) {
	var result application.PlayRecord
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT id,state,version,active_duration_ms
FROM play_sessions WHERE launch_session_id=?`, id).Scan(&result.ID, &result.State, &result.Version,
		&result.ActiveDurationMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PlayRecord{}, false, nil
	}
	if err != nil {
		return application.PlayRecord{}, false, fmt.Errorf("query current play: %w", err)
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
