package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (service *Service) isolatedRuntimeTicket(sessionID string) (string, string, [32]byte, error) {
	var empty [32]byte
	if service.rpgRuntimeOriginTemplate == "" || strings.Count(service.rpgRuntimeOriginTemplate, "{launchId}") != 1 {
		return "", "", empty, ErrBlocked
	}
	origin := strings.Replace(service.rpgRuntimeOriginTemplate, "{launchId}", sessionID, 1)
	parsed, err := uuid.Parse(sessionID)
	if err != nil {
		return "", "", empty, ErrBlocked
	}
	capability := service.credentials.Capability(parsed)
	ticketBytes := sha256.Sum256(append([]byte("retrom-provider-bootstrap-v1\x00"), capability[:]...))
	ticket := base64.RawURLEncoding.EncodeToString(ticketBytes[:])
	hash := sha256.Sum256(ticketBytes[:])
	return origin, ticket, hash, nil
}

func (service *Service) lockIsolatedLaunchBootstrapTicket(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, profileID string,
	createdAt int64,
) error {
	origin, _, ticketHash, err := service.isolatedRuntimeTicket(launchID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO isolated_runtime_bootstrap_tickets(
 ticket_sha256,launch_id,profile_id,expected_origin,expires_at_ms,consumed_at_ms)
VALUES(?,?,?,?,?,NULL)
`, ticketHash[:], launchID, profileID, origin, createdAt+60_000); err != nil {
		return fmt.Errorf("lock isolated bootstrap ticket: %w", err)
	}
	return nil
}

func (service *Service) lockIsolatedPreviewBootstrapTicket(
	ctx context.Context,
	transaction *sql.Tx,
	previewID, actorUserID string,
	createdAt int64,
) error {
	var profileID string
	if err := transaction.QueryRowContext(ctx, `SELECT profile_id FROM users WHERE id=?`, actorUserID).
		Scan(&profileID); err != nil {
		return fmt.Errorf("load preview profile: %w", err)
	}
	origin, _, ticketHash, err := service.isolatedRuntimeTicket(previewID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO isolated_runtime_bootstrap_tickets(
 ticket_sha256,preview_id,profile_id,expected_origin,expires_at_ms,consumed_at_ms)
VALUES(?,?,?,?,?,NULL)
`, ticketHash[:], previewID, profileID, origin, createdAt+60_000); err != nil {
		return fmt.Errorf("lock isolated preview bootstrap ticket: %w", err)
	}
	return nil
}

func (service *Service) isolatedRuntimeAccess(ctx context.Context, sessionID string) (string, string, error) {
	origin, ticket, ticketHash, err := service.isolatedRuntimeTicket(sessionID)
	if err != nil {
		return "", "", err
	}
	now := service.now().UnixMilli()
	var valid int
	if err := service.database.QueryRowContext(ctx, `
SELECT
 EXISTS(SELECT 1 FROM isolated_runtime_bootstrap_tickets
  WHERE (launch_id=? OR preview_id=?) AND ticket_sha256=? AND expected_origin=?
    AND consumed_at_ms IS NULL AND expires_at_ms>?)
 OR EXISTS(SELECT 1 FROM isolated_runtime_capabilities
  WHERE (launch_id=? OR preview_id=?) AND expected_origin=? AND revoked_at_ms IS NULL AND expires_at_ms>?)
	`, sessionID, sessionID, ticketHash[:], origin, now,
		sessionID, sessionID, origin, now).Scan(&valid); err != nil || valid != 1 {
		return "", "", ErrBlocked
	}
	return origin, ticket, nil
}
