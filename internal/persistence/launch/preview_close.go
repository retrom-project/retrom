package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/launch"
)

type PreviewClose struct{ database dbapi.DB }

func NewPreviewClose(database dbapi.DB) *PreviewClose { return &PreviewClose{database: database} }

func (repository *PreviewClose) WithPreviewClose(
	ctx context.Context, work func(application.PreviewCloseScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		if err := work(previewCloseRecords{transaction: tx}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit preview close: %w", err)
	}
	return nil
}

type previewCloseRecords struct{ transaction dbapi.Tx }

func (records previewCloseRecords) Preview(
	ctx context.Context, id string,
) (application.PreviewCloseSource, bool, error) {
	var source application.PreviewCloseSource
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT id,credential_sha256,state,hard_expires_at_ms,version FROM runtime_preview_sessions WHERE id=?`, id).
		Scan(&source.ID, &source.Session.CredentialHash, &source.Session.State,
			&source.Session.HardExpiresAtMS, &source.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return source, false, nil
	}
	if err != nil {
		return source, false, fmt.Errorf("read preview close source: %w", err)
	}
	return source, true, nil
}

func (records previewCloseRecords) Finish(ctx context.Context, source application.PreviewCloseSource, now int64) error {
	result, err := sessionstore.ChangePreview(ctx, records.transaction, recordstore.Update{
		Set: `state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1`, Values: []any{now, now},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=? AND state IN ('CREATED','ACTIVE') AND hard_expires_at_ms>?`,
			Args:  []any{source.ID, source.Version, source.Session.State, now},
		},
	})
	if err != nil {
		return fmt.Errorf("persist preview finish: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count preview finish: %w", err)
	}
	if count != 1 {
		return application.ErrCredential
	}
	return nil
}
