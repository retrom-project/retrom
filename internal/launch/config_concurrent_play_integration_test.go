//go:build integration

package launch

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"
)

func TestConfigConcurrentActivationThenPlayKeepsValidIssuance(t *testing.T) {
	t.Parallel()
	for _, reportAgain := range []bool{false, true} {
		name := "first-sample"
		if reportAgain {
			name = "next-sample"
		}
		t.Run(name, func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, false, false)
			var beforeVersion, currentVersion int64
			if err := dbapi.QueryRowContext(t.Context(), fixture.database,
				`SELECT version FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&beforeVersion); err != nil {
				t.Fatal(err)
			}
			var advanced map[string]string
			builder := configBuildHook{ConfigBuilder: fixture.launcher.runtimeBuilder, after: func() {
				// Issuer A already read CREATED. Issuer B activates, and its
				// ordinary Player performs cumulative statistics before and after a reporting interval.
				if err := configDraftFetch(t, fixture, created, false); err != nil {
					t.Fatal(err)
				}
				productPlayStart(t, fixture, created)
				if reportAgain {
					*fixture.now = fixture.now.Add(time.Second)
					result, err := fixture.launcher.RecordPlaySnapshot(t.Context(), created.LaunchID, created.Capability, PlaySnapshot{ActiveDurationMS: 1000})
					if err != nil || result.ActiveDurationMS != 1000 {
						t.Fatalf("progress=%#v error=%v", result, err)
					}
				}
				if err := fixture.launcher.AuthorizeSave(t.Context(), created.LaunchID, created.Capability); err != nil {
					t.Fatalf("current capability unexpectedly invalid: %v", err)
				}
				if err := dbapi.QueryRowContext(t.Context(), fixture.database,
					`SELECT version FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&currentVersion); err != nil {
					t.Fatal(err)
				}
				advance := int64(1)
				if currentVersion != beforeVersion+advance {
					t.Fatalf("real version progression: before=%d after=%d want increment=%d", beforeVersion, currentVersion, advance)
				}
				advanced = playRows(t, fixture.database)
			}}
			issuer := fixtureConfigIssuer(fixture, persistence.NewConfig(fixture.database), builder)
			configuration, err := issuer.Issue(t.Context(), application.SessionRef{ID: created.LaunchID}, created.Capability)
			if err != nil {
				t.Fatalf("valid config after concurrent activation/play rejected: version %d -> %d: %v", beforeVersion, currentVersion, err)
			}
			if _, err := configuration.MarshalJSON(); err != nil {
				t.Fatal(err)
			}
			configDraftUnchanged(t, fixture, advanced)
		})
	}
}
