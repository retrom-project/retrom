package isolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestResolveHostRequiresCanonicalUUIDAsCompleteLeftmostLabel(t *testing.T) {
	t.Parallel()
	service := New(nil, "http://{launchId}.rpg.feature-a1b2c3d4e5f6.localhost:3000", time.Now)
	launchID := "018fdb34-4f5d-7abc-8def-0123456789ab"
	access, ok := service.ResolveHost(launchID + ".rpg.feature-a1b2c3d4e5f6.localhost:3000")
	if !ok || access.LaunchID != launchID || access.Origin != "http://"+launchID+".rpg.feature-a1b2c3d4e5f6.localhost:3000" {
		t.Fatalf("resolved access = %#v, %t", access, ok)
	}
	for _, host := range []string{
		strings.ToUpper(launchID) + ".rpg.feature-a1b2c3d4e5f6.localhost:3000",
		"prefix." + launchID + ".rpg.feature-a1b2c3d4e5f6.localhost:3000",
		launchID + ".extra.rpg.feature-a1b2c3d4e5f6.localhost:3000",
		"not-a-uuid.rpg.feature-a1b2c3d4e5f6.localhost:3000",
		launchID + ".rpg.feature-a1b2c3d4e5f6.localhost:443",
	} {
		if _, accepted := service.ResolveHost(host); accepted {
			t.Fatalf("invalid runtime host accepted: %s", host)
		}
	}
}

func TestRuntimeHostCandidateFailsClosedWithoutClaimingApplicationHosts(t *testing.T) {
	t.Parallel()
	service := New(nil, "http://{launchId}.rpg.localhost:8080", time.Now)
	if !service.IsRuntimeHostCandidate("invalid.rpg.localhost:8080") {
		t.Fatal("invalid runtime-suffix host was not recognized as a candidate")
	}
	for _, host := range []string{"localhost:8080", "app.localhost:3000", "rpg.localhost:8080"} {
		if service.IsRuntimeHostCandidate(host) {
			t.Fatalf("application host claimed as runtime candidate: %s", host)
		}
	}
}

func TestBootstrapTicketIsSingleUseAndCapabilityRevocationIsTerminal(t *testing.T) {
	t.Parallel()
	fixture := newIsolationFixture(t)
	if _, err := fixture.service.InspectBootstrap(
		context.Background(), fixture.launchID, fixture.origin,
	); err != nil {
		t.Fatal(err)
	}
	credential, access, err := fixture.service.ConsumeTicket(
		context.Background(), fixture.launchID, fixture.origin, fixture.ticket,
	)
	if err != nil || access.LaunchID != fixture.launchID || access.Origin != fixture.origin ||
		access.ContentFormat != "RPG_MAKER_PROJECT_V1" || access.Preview {
		t.Fatalf("consume ticket = (%q,%#v,%v)", credential, access, err)
	}
	if _, err := fixture.service.InspectBootstrap(
		context.Background(), fixture.launchID, fixture.origin,
	); !errors.Is(err, ErrCredential) {
		t.Fatalf("consumed bootstrap inspect error = %v", err)
	}
	if _, _, err := fixture.service.ConsumeTicket(
		context.Background(), fixture.launchID, fixture.origin, fixture.ticket,
	); !errors.Is(err, ErrCredential) {
		t.Fatalf("ticket replay error = %v", err)
	}
	authorized, err := fixture.service.Authenticate(
		context.Background(), fixture.launchID, fixture.origin, credential,
	)
	if err != nil || authorized.Profile != "profile" {
		t.Fatalf("authenticate capability = (%#v,%v)", authorized, err)
	}
	assertInvalidCapabilities(t, fixture, credential)
	assertRevokedCapability(t, fixture, credential, authorized)
}

func assertInvalidCapabilities(t *testing.T, fixture isolationFixture, credential string) {
	t.Helper()
	for _, invalid := range []struct{ launchID, origin, credential string }{
		{fixture.launchID, fixture.origin, "invalid"},
		{"01980000-0000-7000-8000-000000000092", fixture.origin, credential},
		{fixture.launchID, "https://wrong.example", credential},
	} {
		if _, err := fixture.service.Authenticate(
			context.Background(), invalid.launchID, invalid.origin, invalid.credential,
		); !errors.Is(err, ErrCredential) {
			t.Fatalf("invalid capability authentication error = %v", err)
		}
	}
}

func assertRevokedCapability(t *testing.T, fixture isolationFixture, credential string, authorized Access) {
	t.Helper()
	if err := fixture.service.Revoke(context.Background(), authorized); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Authenticate(
		context.Background(), fixture.launchID, fixture.origin, credential,
	); !errors.Is(err, ErrCredential) {
		t.Fatalf("revoked capability authentication error = %v", err)
	}
	if err := fixture.service.Revoke(context.Background(), authorized); !errors.Is(err, ErrCredential) {
		t.Fatalf("repeated capability revocation error = %v", err)
	}
}

func TestTyranoScriptPreviewTicketCreatesPreviewScopedCapability(t *testing.T) {
	t.Parallel()
	fixture := newIsolationPreviewFixture(t)
	access, err := fixture.service.InspectBootstrap(
		context.Background(), fixture.launchID, fixture.origin,
	)
	if err != nil || access.ContentFormat != "TYRANOSCRIPT_PROJECT_V1" || !access.Preview {
		t.Fatalf("inspect preview bootstrap = (%#v,%v)", access, err)
	}
	credential, consumed, err := fixture.service.ConsumeTicket(
		context.Background(), fixture.launchID, fixture.origin, fixture.ticket,
	)
	if err != nil || consumed.ContentFormat != "TYRANOSCRIPT_PROJECT_V1" || !consumed.Preview {
		t.Fatalf("consume preview bootstrap = (%q,%#v,%v)", credential, consumed, err)
	}
	authorized, err := fixture.service.Authenticate(
		context.Background(), fixture.launchID, fixture.origin, credential,
	)
	if err != nil || authorized.Profile != "profile" || authorized.ContentFormat != "TYRANOSCRIPT_PROJECT_V1" ||
		!authorized.Preview {
		t.Fatalf("authenticate preview capability = (%#v,%v)", authorized, err)
	}
	if err := fixture.service.Revoke(context.Background(), authorized); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Authenticate(
		context.Background(), fixture.launchID, fixture.origin, credential,
	); !errors.Is(err, ErrCredential) {
		t.Fatalf("revoked preview capability error = %v", err)
	}
}

func TestBootstrapAndCapabilityExpiryFailClosed(t *testing.T) {
	t.Parallel()
	t.Run("bootstrap ticket", func(t *testing.T) {
		fixture := newIsolationFixture(t)
		*fixture.nowMS += 60_000
		if _, err := fixture.service.InspectBootstrap(
			context.Background(), fixture.launchID, fixture.origin,
		); !errors.Is(err, ErrCredential) {
			t.Fatalf("expired bootstrap inspect error = %v", err)
		}
		if _, _, err := fixture.service.ConsumeTicket(
			context.Background(), fixture.launchID, fixture.origin, fixture.ticket,
		); !errors.Is(err, ErrCredential) {
			t.Fatalf("expired bootstrap consumption error = %v", err)
		}
	})
	t.Run("isolated capability", func(t *testing.T) {
		fixture := newIsolationFixture(t)
		credential, _, err := fixture.service.ConsumeTicket(
			context.Background(), fixture.launchID, fixture.origin, fixture.ticket,
		)
		if err != nil {
			t.Fatal(err)
		}
		*fixture.nowMS += 120_000
		if _, err := fixture.service.Authenticate(
			context.Background(), fixture.launchID, fixture.origin, credential,
		); !errors.Is(err, ErrCredential) {
			t.Fatalf("expired capability authentication error = %v", err)
		}
	})
}

type isolationFixture struct {
	service  *Service
	nowMS    *int64
	launchID string
	origin   string
	ticket   string
}

func newIsolationFixture(t *testing.T) isolationFixture {
	return newIsolationFixtureForSession(t, false)
}

func newIsolationPreviewFixture(t *testing.T) isolationFixture {
	return newIsolationFixtureForSession(t, true)
}

func newIsolationFixtureForSession(t *testing.T, preview bool) isolationFixture {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
CREATE TABLE launch_sessions(
 id TEXT PRIMARY KEY,profile_id TEXT,state TEXT,hard_expires_at_ms INTEGER
);
CREATE TABLE launch_content_files(
 launch_session_id TEXT,logical_name TEXT,format_version TEXT
);
CREATE TABLE review_preview_sessions(
 id TEXT PRIMARY KEY,state TEXT,hard_expires_at_ms INTEGER,content_format TEXT
);
CREATE TABLE isolated_runtime_bootstrap_tickets(
 ticket_sha256 BLOB,launch_id TEXT,preview_id TEXT,profile_id TEXT,expected_origin TEXT,
 expires_at_ms INTEGER,consumed_at_ms INTEGER
);
CREATE TABLE isolated_runtime_capabilities(
 credential_sha256 BLOB,launch_id TEXT,preview_id TEXT,profile_id TEXT,expected_origin TEXT,
 issued_at_ms INTEGER,expires_at_ms INTEGER,revoked_at_ms INTEGER
);`); err != nil {
		t.Fatal(err)
	}
	const launchID = "01980000-0000-7000-8000-000000000091"
	const origin = "https://01980000-0000-7000-8000-000000000091.rpg-runtime.example"
	nowMS := int64(10_000)
	ticketBytes := bytes.Repeat([]byte{0x5a}, 32)
	ticket := base64.RawURLEncoding.EncodeToString(ticketBytes)
	ticketDigest := sha256.Sum256(ticketBytes)
	statements := []struct {
		query     string
		arguments []any
	}{
		{`INSERT INTO launch_sessions VALUES(?,'profile','ACTIVE',?)`, []any{launchID, nowMS + 120_000}},
		{`INSERT INTO launch_content_files VALUES(?,'index.html','RPG_MAKER_PROJECT_V1')`, []any{launchID}},
		{`INSERT INTO isolated_runtime_bootstrap_tickets VALUES(?,?,NULL,'profile',?,?,NULL)`, []any{ticketDigest[:], launchID, origin, nowMS + 60_000}},
	}
	if preview {
		statements = []struct {
			query     string
			arguments []any
		}{
			{`INSERT INTO review_preview_sessions VALUES(?,'ACTIVE',?,'TYRANOSCRIPT_PROJECT_V1')`, []any{launchID, nowMS + 120_000}},
			{`INSERT INTO isolated_runtime_bootstrap_tickets VALUES(?,NULL,?,'profile',?,?,NULL)`, []any{ticketDigest[:], launchID, origin, nowMS + 60_000}},
		}
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement.query, statement.arguments...); err != nil {
			t.Fatal(err)
		}
	}
	service := New(database, "https://{launchId}.rpg-runtime.example", func() time.Time {
		return time.UnixMilli(nowMS)
	})
	return isolationFixture{
		service: service, nowMS: &nowMS, launchID: launchID, origin: origin, ticket: ticket,
	}
}
