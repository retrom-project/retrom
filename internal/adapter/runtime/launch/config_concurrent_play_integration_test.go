//go:build integration

package launch

import (
	"testing"
	"time"

	launchmodel "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
)

func TestConfigConcurrentActivationThenPlayKeepsValidIssuance(t *testing.T) {
	t.Parallel()
	for _, heartbeat := range []bool{false, true} {
		name := "start"
		if heartbeat {
			name = "heartbeat"
		}
		t.Run(name, func(t *testing.T) {
			testConfigConcurrentActivationThenPlay(t, heartbeat)
		})
	}
}

func testConfigConcurrentActivationThenPlay(t *testing.T, heartbeat bool) {
	fixture, created := newPlaySourceFixture(t, false, false)
	var beforeVersion, currentVersion int64
	if err := fixture.database.QueryRowContext(t.Context(),
		`SELECT version FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	var advanced map[string]string
	builder := configBuildHook{ConfigBuilder: fixture.launcher.runtimeBuilder, after: func() {
		// Issuer A already read CREATED. Issuer B activates, and its
		// ordinary Player performs real START and optionally HEARTBEAT.
		if err := configDraftFetch(t, fixture, created, false); err != nil {
			t.Fatal(err)
		}
		productPlayStart(t, fixture, created)
		if heartbeat {
			*fixture.now = fixture.now.Add(time.Second)
			result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability,
				"heartbeat", PlayEvent{
					ClientSequence: 1, ClientObservedAtMS: fixture.now.UnixMilli(),
					PreviousInterval: &Interval{Running: true, Visible: true},
				})
			if err != nil || result.State != "ACTIVE" || result.AcceptedDuration != 1000 {
				t.Fatalf("legitimate heartbeat: state=%s duration=%d error=%v", result.State, result.AcceptedDuration, err)
			}
		}
		if _, err := fixture.launcher.SaveAccess(t.Context(), created.LaunchID, created.Capability); err != nil {
			t.Fatalf("current capability unexpectedly invalid: %v", err)
		}
		if err := fixture.database.QueryRowContext(t.Context(),
			`SELECT version FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&currentVersion); err != nil {
			t.Fatal(err)
		}
		advance := int64(2)
		if heartbeat {
			advance++
		}
		if currentVersion != beforeVersion+advance {
			t.Fatalf("real version progression: before=%d after=%d want increment=%d", beforeVersion, currentVersion, advance)
		}
		advanced = playRows(t, fixture.database)
	}}
	issuer := fixtureConfigIssuer(fixture, persistence.NewConfig(fixture.database), builder)
	configuration, err := issuer.Issue(t.Context(), launchmodel.SessionRef{ID: created.LaunchID}, created.Capability)
	if err != nil {
		t.Fatalf("valid config after concurrent activation/play rejected: version %d -> %d: %v", beforeVersion, currentVersion, err)
	}
	if _, err := configuration.MarshalJSON(); err != nil {
		t.Fatal(err)
	}
	configDraftUnchanged(t, fixture, advanced)
}
