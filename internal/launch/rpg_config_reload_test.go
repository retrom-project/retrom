package launch

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	retromruntime "retrom/internal/runtime"
)

func TestNativeRuntimeAccessAllowsOnlyBootstrapOrLiveIsolatedCapability(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
CREATE TABLE isolated_runtime_bootstrap_tickets(
 ticket_sha256 BLOB,launch_id TEXT,expected_origin TEXT,consumed_at_ms INTEGER,expires_at_ms INTEGER
);
CREATE TABLE isolated_runtime_capabilities(
 launch_id TEXT,expected_origin TEXT,expires_at_ms INTEGER,revoked_at_ms INTEGER
);`); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(10_000)
	service := New(database, nil, credentials, func() time.Time { return now }).
		WithRPGRuntimeOriginTemplate("https://{launchId}.rpg-runtime.example")
	launchID := "01980000-0000-7000-8000-000000000091"
	origin, ticket, ticketHash, err := service.nativeRuntimeTicket(launchID)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeRuntimeAccessBlocked(t, service, launchID)
	if _, err := database.ExecContext(ctx, `
INSERT INTO isolated_runtime_bootstrap_tickets(
 ticket_sha256,launch_id,expected_origin,consumed_at_ms,expires_at_ms
) VALUES(?,?,?,NULL,?)`, ticketHash[:], launchID, origin, now.UnixMilli()+60_000); err != nil {
		t.Fatal(err)
	}
	assertNativeRuntimeAccess(t, service, launchID, origin, ticket)
	if _, err := database.ExecContext(ctx,
		`UPDATE isolated_runtime_bootstrap_tickets SET consumed_at_ms=?`, now.UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	assertNativeRuntimeAccessBlocked(t, service, launchID)
	if _, err := database.ExecContext(ctx, `
INSERT INTO isolated_runtime_capabilities(launch_id,expected_origin,expires_at_ms,revoked_at_ms)
VALUES(?,?,?,NULL)`, launchID, origin, now.UnixMilli()+120_000); err != nil {
		t.Fatal(err)
	}
	assertNativeRuntimeAccess(t, service, launchID, origin, ticket)
	for _, mutation := range []string{
		`UPDATE isolated_runtime_capabilities SET expected_origin='https://wrong.example'`,
		`UPDATE isolated_runtime_capabilities SET expected_origin='` + origin + `',expires_at_ms=10000`,
		`UPDATE isolated_runtime_capabilities SET expires_at_ms=130000,revoked_at_ms=10001`,
	} {
		if _, err := database.ExecContext(ctx, mutation); err != nil {
			t.Fatal(err)
		}
		assertNativeRuntimeAccessBlocked(t, service, launchID)
	}
}

func assertNativeRuntimeAccess(
	t *testing.T,
	service *Service,
	launchID, wantOrigin, wantTicket string,
) {
	t.Helper()
	origin, ticket, err := service.nativeRuntimeAccess(context.Background(), launchID)
	if err != nil || origin != wantOrigin || ticket != wantTicket {
		t.Fatalf("native runtime access = (%q,%q,%v)", origin, ticket, err)
	}
}

func assertNativeRuntimeAccessBlocked(t *testing.T, service *Service, launchID string) {
	t.Helper()
	if _, _, err := service.nativeRuntimeAccess(context.Background(), launchID); !errors.Is(err, ErrBlocked) {
		t.Fatalf("native runtime access error = %v, want %v", err, ErrBlocked)
	}
}
