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
