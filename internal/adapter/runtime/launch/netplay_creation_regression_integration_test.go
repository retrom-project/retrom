//go:build integration

package launch

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"
)

func TestNetplayCreationPreservesPreparationFailure(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	cause := errors.New("netplay snapshot unavailable")
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(
				query,
				"FROM netplay_sessions session",
			) && len(
				args,
			) > 0 && args[0].Value == fixture.request.SessionID {
				hits++
				return cause
			}
			return nil
		},
	})
	result, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if hits != 1 || !errors.Is(err, cause) || result.LaunchID != "" {
		t.Fatalf("snapshot failure hits=%d result=%q error=%v", hits, result.LaunchID, err)
	}
	assertNetplayLaunchCount(t, fixture, 0)
}

func TestNetplayCreationPreservesCancellation(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := fixture.service.CreateNetplay(ctx, fixture.request)
	if !errors.Is(err, context.Canceled) || result.LaunchID != "" {
		t.Fatalf("cancellation result=%q error=%v", result.LaunchID, err)
	}
	assertNetplayLaunchCount(t, fixture, 0)
}

func TestNetplayCreationRejectsSessionFinishedAfterPreparation(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	invoked := false
	fixture.service.now = func() time.Time {
		if !invoked {
			invoked = true
			mustRPGLaunchSQL(
				t,
				fixture.database,
				`UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason='PREPARE_FAILED',version=version+1 WHERE id=?`,
				fixture.now().UnixMilli(),
				fixture.request.SessionID,
			)
		}
		return fixture.now()
	}
	result, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if !invoked || !errors.Is(err, ErrBlocked) || result.LaunchID != "" {
		t.Fatalf("closed session hook=%v result=%q error=%v", invoked, result.LaunchID, err)
	}
	assertNetplayLaunchCount(t, fixture, 0)
}

func assertNetplayLaunchCount(t *testing.T, fixture netplayLaunchFixture, want int) {
	t.Helper()
	var launches int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM launch_sessions WHERE netplay_session_id=?`, fixture.request.SessionID).Scan(

		&launches,
	); err != nil {
		t.Fatal(err)
	}
	if launches != want {
		t.Fatalf("netplay launches=%d want=%d", launches, want)
	}
}
