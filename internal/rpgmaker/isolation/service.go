package isolation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

var ErrCredential = errors.New("RPG_ISOLATED_RUNTIME_CREDENTIAL_INVALID")

const originMarker = "00000000-0000-4000-8000-000000000000"

type Service struct {
	database *sql.DB
	now      func() time.Time
	template string
}

type Access struct {
	LaunchID      string
	Origin        string
	Profile       string
	ContentFormat string
	Preview       bool
	Expires       int64
}

func New(database *sql.DB, template string, now func() time.Time) *Service {
	return &Service{database: database, template: template, now: now}
}

func (service *Service) ResolveHost(host string) (Access, bool) {
	parsed, suffix, ok := service.runtimeTemplate()
	if !ok || !strings.HasSuffix(host, suffix) {
		return Access{}, false
	}
	launchID := strings.TrimSuffix(host, suffix)
	parsedID, err := uuid.Parse(launchID)
	if err != nil || parsedID.String() != launchID || launchID+suffix != host {
		return Access{}, false
	}
	origin := parsed.Scheme + "://" + host
	return Access{LaunchID: launchID, Origin: origin}, true
}

func (service *Service) IsRuntimeHostCandidate(host string) bool {
	_, suffix, ok := service.runtimeTemplate()
	return ok && strings.HasSuffix(host, suffix)
}

func (service *Service) runtimeTemplate() (*url.URL, string, bool) {
	concrete := strings.Replace(service.template, "{launchId}", originMarker, 1)
	parsed, err := url.Parse(concrete)
	if err != nil || parsed.Host == "" || !strings.HasPrefix(parsed.Host, originMarker) {
		return nil, "", false
	}
	suffix := strings.TrimPrefix(parsed.Host, originMarker)
	return parsed, suffix, suffix != ""
}

func (service *Service) InspectBootstrap(ctx context.Context, launchID, origin string) (Access, error) {
	var access Access
	err := service.database.QueryRowContext(ctx, `
SELECT ticket.profile_id,ticket.expires_at_ms,
 COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
                                  WHERE file.launch_session_id=launch.id)),
 ticket.preview_id IS NOT NULL
FROM isolated_runtime_bootstrap_tickets ticket
LEFT JOIN launch_sessions launch ON launch.id=ticket.launch_id
LEFT JOIN review_preview_sessions preview ON preview.id=ticket.preview_id
WHERE COALESCE(ticket.launch_id,ticket.preview_id)=? AND ticket.expected_origin=?
  AND ticket.consumed_at_ms IS NULL AND ticket.expires_at_ms>?
  AND (ticket.launch_id IS NOT NULL AND launch.state='ACTIVE' AND launch.hard_expires_at_ms>?
    OR ticket.preview_id IS NOT NULL AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?)
  AND COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
      WHERE file.launch_session_id=launch.id)) IN ('RPG_MAKER_PROJECT','TYRANOSCRIPT_PROJECT')
`, launchID, origin, service.now().UnixMilli(), service.now().UnixMilli(), service.now().UnixMilli()).Scan(
		&access.Profile, &access.Expires, &access.ContentFormat, &access.Preview,
	)
	if err != nil {
		return Access{}, ErrCredential
	}
	access.LaunchID, access.Origin = launchID, origin
	return access, nil
}

func (service *Service) ConsumeTicket(
	ctx context.Context, launchID, origin, ticket string,
) (string, Access, error) {
	ticketBytes, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(ticketBytes) != 32 || base64.RawURLEncoding.EncodeToString(ticketBytes) != ticket {
		return "", Access{}, ErrCredential
	}
	ticketDigest := sha256.Sum256(ticketBytes)
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", Access{}, fmt.Errorf("begin isolated runtime bootstrap: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var access Access
	err = transaction.QueryRowContext(ctx, `
SELECT ticket.profile_id,COALESCE(launch.hard_expires_at_ms,preview.hard_expires_at_ms),
 COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
                                  WHERE file.launch_session_id=launch.id)),
 ticket.preview_id IS NOT NULL
FROM isolated_runtime_bootstrap_tickets ticket
LEFT JOIN launch_sessions launch ON launch.id=ticket.launch_id
LEFT JOIN review_preview_sessions preview ON preview.id=ticket.preview_id
WHERE COALESCE(ticket.launch_id,ticket.preview_id)=? AND ticket.ticket_sha256=? AND ticket.expected_origin=?
  AND ticket.consumed_at_ms IS NULL AND ticket.expires_at_ms>?
  AND (ticket.launch_id IS NOT NULL AND launch.state='ACTIVE' AND launch.hard_expires_at_ms>?
    OR ticket.preview_id IS NOT NULL AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?)
  AND COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
      WHERE file.launch_session_id=launch.id)) IN ('RPG_MAKER_PROJECT','TYRANOSCRIPT_PROJECT')
`, launchID, ticketDigest[:], origin, now, now, now).Scan(
		&access.Profile, &access.Expires, &access.ContentFormat, &access.Preview,
	)
	if err != nil {
		return "", Access{}, ErrCredential
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE isolated_runtime_bootstrap_tickets SET consumed_at_ms=?
WHERE COALESCE(launch_id,preview_id)=? AND ticket_sha256=? AND consumed_at_ms IS NULL AND expires_at_ms>?
`, now, launchID, ticketDigest[:], now)
	if err != nil {
		return "", Access{}, fmt.Errorf("consume isolated runtime ticket: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return "", Access{}, ErrCredential
	}
	credentialBytes := make([]byte, 32)
	if _, err := rand.Read(credentialBytes); err != nil {
		return "", Access{}, fmt.Errorf("generate isolated runtime credential: %w", err)
	}
	credential := base64.RawURLEncoding.EncodeToString(credentialBytes)
	credentialDigest := sha256.Sum256(credentialBytes)
	var capabilityLaunchID, capabilityPreviewID any = launchID, nil
	if access.Preview {
		capabilityLaunchID, capabilityPreviewID = nil, launchID
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO isolated_runtime_capabilities(
 credential_sha256,launch_id,preview_id,profile_id,expected_origin,issued_at_ms,expires_at_ms,revoked_at_ms)
VALUES(?,?,?,?,?,?,?,NULL)
`, credentialDigest[:], capabilityLaunchID, capabilityPreviewID,
		access.Profile, origin, now, access.Expires)
	if err != nil {
		return "", Access{}, fmt.Errorf("create isolated runtime credential: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", Access{}, fmt.Errorf("commit isolated runtime bootstrap: %w", err)
	}
	access.LaunchID, access.Origin = launchID, origin
	return credential, access, nil
}

func (service *Service) Authenticate(
	ctx context.Context, launchID, origin, credential string,
) (Access, error) {
	credentialBytes, err := base64.RawURLEncoding.DecodeString(credential)
	if err != nil || len(credentialBytes) != 32 || base64.RawURLEncoding.EncodeToString(credentialBytes) != credential {
		return Access{}, ErrCredential
	}
	digest := sha256.Sum256(credentialBytes)
	var access Access
	err = service.database.QueryRowContext(ctx, `
SELECT capability.profile_id,capability.expires_at_ms,
 COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
                                  WHERE file.launch_session_id=launch.id)),
 capability.preview_id IS NOT NULL
FROM isolated_runtime_capabilities capability
LEFT JOIN launch_sessions launch ON launch.id=capability.launch_id
LEFT JOIN review_preview_sessions preview ON preview.id=capability.preview_id
WHERE capability.credential_sha256=? AND COALESCE(capability.launch_id,capability.preview_id)=?
  AND capability.expected_origin=? AND capability.revoked_at_ms IS NULL AND capability.expires_at_ms>?
  AND (capability.launch_id IS NOT NULL AND launch.state='ACTIVE' AND launch.hard_expires_at_ms>?
    OR capability.preview_id IS NOT NULL AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?)
  AND COALESCE(preview.content_format,(SELECT min(file.format_version) FROM launch_content_files file
      WHERE file.launch_session_id=launch.id)) IN ('RPG_MAKER_PROJECT','TYRANOSCRIPT_PROJECT')
`, digest[:], launchID, origin, service.now().UnixMilli(), service.now().UnixMilli(), service.now().UnixMilli()).Scan(
		&access.Profile, &access.Expires, &access.ContentFormat, &access.Preview,
	)
	if err != nil {
		return Access{}, ErrCredential
	}
	access.LaunchID, access.Origin = launchID, origin
	return access, nil
}

func (service *Service) Revoke(ctx context.Context, access Access) error {
	result, err := service.database.ExecContext(ctx, `
UPDATE isolated_runtime_capabilities SET revoked_at_ms=?
WHERE COALESCE(launch_id,preview_id)=? AND expected_origin=? AND revoked_at_ms IS NULL
`, service.now().UnixMilli(), access.LaunchID, access.Origin)
	if err != nil {
		return fmt.Errorf("revoke isolated runtime credential: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrCredential
	}
	return nil
}
