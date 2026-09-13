package isolation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/isolation"
)

type (
	Repository struct{ database *sql.DB }
	tickets    struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) WithWrite(ctx context.Context, work func(isolation.Tickets) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("isolation/begin: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(tickets{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("isolation/commit: %w", err)
	}
	return nil
}

func (repository *Repository) Bootstrap(ctx context.Context, query isolation.TicketQuery) (isolation.Bootstrap, error) {
	return tickets{executor: repository.database}.Bootstrap(ctx, query)
}

func (store tickets) Bootstrap(ctx context.Context, query isolation.TicketQuery) (isolation.Bootstrap, error) {
	var result isolation.Bootstrap
	var digest any
	if query.Digest != nil {
		digest = query.Digest[:]
	}
	err := store.executor.QueryRowContext(ctx, `
SELECT ticket.profile_id,ticket.expires_at_ms,ticket.consumed_at_ms IS NOT NULL,
 COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
                                 WHERE file.launch_session_id=launch.id),''),
 ticket.preview_id IS NOT NULL,COALESCE(launch.state,preview.state,''),
 COALESCE(launch.hard_expires_at_ms,preview.hard_expires_at_ms,0)
FROM isolated_runtime_bootstrap_tickets ticket
LEFT JOIN launch_sessions launch ON launch.id=ticket.launch_id
LEFT JOIN review_preview_sessions preview ON preview.id=ticket.preview_id
WHERE COALESCE(ticket.launch_id,ticket.preview_id)=? AND ticket.expected_origin=?
 AND (? IS NULL OR ticket.ticket_sha256=?)
`, query.LaunchID, query.Origin, digest, digest).Scan(&result.Session.Profile, &result.ExpiresAtMS, &result.Consumed,
		&result.Session.ContentFormat, &result.Session.Preview, &result.Session.State, &result.Session.HardExpiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return isolation.Bootstrap{}, isolation.ErrCredential
	}
	if err != nil {
		return isolation.Bootstrap{}, fmt.Errorf("isolation/read bootstrap: %w", err)
	}
	return result, nil
}

func (store tickets) Consume(ctx context.Context, query isolation.TicketQuery, now int64) error {
	if query.Digest == nil {
		return isolation.ErrCredential
	}
	result, err := recordstore.UpdateIsolatedRuntimeBootstrapTickets(ctx, store.executor, recordstore.Update{
		Set: `consumed_at_ms=?`, Scope: recordstore.Scope{
			Where: `COALESCE(launch_id,preview_id)=? AND expected_origin=? AND ticket_sha256=?
AND consumed_at_ms IS NULL AND expires_at_ms>?`,
			Args: []any{query.LaunchID, query.Origin, query.Digest[:], now},
		}, Values: []any{now},
	})
	return requireChanged(result, err)
}

func (store tickets) Issue(ctx context.Context, write isolation.CapabilityWrite) error {
	var launchID, previewID any = write.Access.LaunchID, nil
	if write.Access.Preview {
		launchID, previewID = nil, write.Access.LaunchID
	}
	_, err := recordstore.CreateIsolatedRuntimeCapabilities(
		ctx,
		store.executor,
		`
INSERT INTO isolated_runtime_capabilities(
 credential_sha256,launch_id,preview_id,profile_id,expected_origin,issued_at_ms,expires_at_ms,revoked_at_ms)
VALUES(?,?,?,?,?,?,?,NULL)
`,
		write.Digest[:],
		launchID,
		previewID,
		write.Access.Profile,
		write.Access.Origin,
		write.IssuedAtMS,
		write.Access.Expires,
	)
	if err != nil {
		return fmt.Errorf("isolation/insert capability: %w", err)
	}
	return nil
}

func (repository *Repository) Capability(
	ctx context.Context,
	query isolation.CredentialQuery,
) (isolation.Capability, error) {
	var result isolation.Capability
	err := repository.database.QueryRowContext(ctx, `
SELECT capability.profile_id,capability.expires_at_ms,capability.revoked_at_ms IS NOT NULL,
 COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
                                 WHERE file.launch_session_id=launch.id),''),
 capability.preview_id IS NOT NULL,COALESCE(launch.state,preview.state,''),
 COALESCE(launch.hard_expires_at_ms,preview.hard_expires_at_ms,0)
FROM isolated_runtime_capabilities capability
LEFT JOIN launch_sessions launch ON launch.id=capability.launch_id
LEFT JOIN review_preview_sessions preview ON preview.id=capability.preview_id
WHERE capability.credential_sha256=? AND COALESCE(capability.launch_id,capability.preview_id)=?
 AND capability.expected_origin=?
`, query.Digest[:], query.LaunchID, query.Origin).Scan(&result.Session.Profile, &result.ExpiresAtMS, &result.Revoked,
		&result.Session.ContentFormat, &result.Session.Preview, &result.Session.State, &result.Session.HardExpiresAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return isolation.Capability{}, isolation.ErrCredential
	}
	if err != nil {
		return isolation.Capability{}, fmt.Errorf("isolation/read capability: %w", err)
	}
	return result, nil
}

func (repository *Repository) Revoke(ctx context.Context, access isolation.Access, now int64) error {
	result, err := recordstore.UpdateIsolatedRuntimeCapabilities(ctx, repository.database, recordstore.Update{
		Set: `revoked_at_ms=?`, Scope: recordstore.Scope{
			Where: `COALESCE(launch_id,preview_id)=? AND expected_origin=? AND revoked_at_ms IS NULL`,
			Args:  []any{access.LaunchID, access.Origin},
		}, Values: []any{now},
	})
	return requireChanged(result, err)
}

func requireChanged(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("isolation/write: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("isolation/count changes: %w", err)
	}
	if count != 1 {
		return isolation.ErrCredential
	}
	return nil
}
