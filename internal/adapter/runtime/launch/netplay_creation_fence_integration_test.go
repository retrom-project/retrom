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

func TestNetplayCreationRejectsChangedFinalInputs(t *testing.T) {
	cases := []struct {
		name, query string
		argument    func(NetplayCreateRequest) string
	}{
		{
			"content name",
			`UPDATE game_files SET logical_name='changed.nes' WHERE game_id=? AND role='CONTENT'`,
			func(r NetplayCreateRequest) string { return r.GameID },
		},
		{
			"source manifest",
			`UPDATE games SET source_manifest_digest='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',version=version+1 WHERE id=?`,
			func(r NetplayCreateRequest) string { return r.GameID },
		},
		{
			"disabled instance",
			`UPDATE platform_instances SET enabled=0 WHERE id=(SELECT platform_instance_id FROM games WHERE id=?)`,
			func(r NetplayCreateRequest) string { return r.GameID },
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fixture := newNetplayLaunchFixture(t)
			invoked := false
			fixture.service.now = func() time.Time {
				if !invoked {
					invoked = true
					mustRPGLaunchSQL(t, fixture.database, item.query, item.argument(fixture.request))
				}
				return fixture.now()
			}
			result, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
			if !invoked || !errors.Is(err, ErrBlocked) || result.LaunchID != "" {
				t.Fatalf("final input hook=%t launch=%q error=%v", invoked, result.LaunchID, err)
			}
			assertNetplayLaunchCount(t, fixture, 0)
		})
	}
}

func TestNetplayCreationAllowsConcurrentMetadataEdit(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	invoked := false
	fixture.service.now = func() time.Time {
		if !invoked {
			invoked = true
			mustRPGLaunchSQL(
				t,
				fixture.database,
				`UPDATE games SET title='Renamed during preparation',version=version+1 WHERE id=?`,
				fixture.request.GameID,
			)
		}
		return fixture.now()
	}
	created, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if !invoked || err != nil || created.LaunchID == "" {
		t.Fatalf("metadata-only edit hook=%t error=%v", invoked, err)
	}
	assertNetplayLaunchCount(t, fixture, 1)
}

type netplayAffectedFailure struct {
	driver.Result
	cause error
}

func (result netplayAffectedFailure) RowsAffected() (int64, error) { return 0, result.cause }

func TestNetplayCreationPreservesBindingCountFailure(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	cause := errors.New("participant affected count unavailable")
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.TrimSpace(query), "UPDATE netplay_session_participants SET") && len(args) >= 7 &&
				args[4].Value == fixture.request.SessionID && args[5].Value == fixture.request.ProfileID {
				hits++
				return netplayAffectedFailure{result, cause}, nil
			}
			return result, nil
		},
	})
	created, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if hits != 1 || !errors.Is(err, cause) || created.LaunchID != "" {
		t.Fatalf("affected failure hits=%d launch=%q error=%v", hits, created.LaunchID, err)
	}
	assertNetplayCreationRolledBack(t, fixture)
}

func assertNetplayCreationRolledBack(t *testing.T, fixture netplayLaunchFixture) {
	t.Helper()
	for _, table := range []string{
		"launch_sessions",
		"launch_content_files",
		"launch_external_files",
		"launch_payload_retirements",
		"launch_game_save_bindings",
	} {
		var count int
		if err := fixture.database.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rollback retained %s=%d", table, count)
		}
	}
	var state string
	var generation, version int
	if err := fixture.database.QueryRowContext(
		t.Context(),
		`SELECT state,credential_generation,version FROM netplay_session_participants WHERE netplay_session_id=? AND profile_id=?`,
		fixture.request.SessionID,
		fixture.request.ProfileID,
	).
		Scan(&state, &generation, &version); err != nil {
		t.Fatal(err)
	}
	if state != "LOCKED" || generation != 0 || version != 1 {
		t.Fatalf("rollback retained participant %s/%d/%d", state, generation, version)
	}
}

func TestNetplayCreationRollsBackContentWriteFailure(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	cause := errors.New("netplay frozen content unavailable")
	var logicalName string
	if err := fixture.database.QueryRowContext(
		t.Context(),
		`SELECT logical_name FROM game_files WHERE game_id=? AND role='CONTENT'`, fixture.request.GameID,
	).Scan(&logicalName); err != nil {
		t.Fatal(err)
	}
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO launch_content_files(") &&
				len(args) == 5 && args[1].Value == logicalName {
				hits++
				return cause
			}
			return nil
		},
	})
	created, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if hits != 1 || !errors.Is(err, cause) || created.LaunchID != "" {
		t.Fatalf("content failure hits=%d launch=%q error=%v", hits, created.LaunchID, err)
	}
	assertNetplayCreationRolledBack(t, fixture)
}
