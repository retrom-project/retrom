//go:build integration

package launch

import (
	"context"
	"errors"
	"reflect"
	"testing"

	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/capability/runtime/runtimelaunch"
	launchmodel "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

type configBuildHook struct {
	launchmodel.ConfigBuilder

	after func()
}

func (builder configBuildHook) Build(input runtimelaunch.Input) ([]byte, error) {
	contents, err := builder.ConfigBuilder.Build(input)
	if err == nil {
		builder.after()
	}
	return contents, err
}

type configRollbackRepository struct {
	launchmodel.ConfigRepository

	cause error
}

func (repository configRollbackRepository) WithActivation(
	ctx context.Context,
	work func(launchmodel.ConfigActivation) error,
) error {
	return repository.ConfigRepository.WithActivation(ctx, func(transaction launchmodel.ConfigActivation) error {
		if err := work(transaction); err != nil {
			return err
		}
		return repository.cause
	})
}

func fixtureConfigIssuer(
	fixture reviewCheckpointFixture,
	repository launchmodel.ConfigRepository,
	builder launchmodel.ConfigBuilder,
) *application.ConfigIssuer {
	return application.NewConfigIssuer(repository, builder, application.ConfigEnvironment{
		Now: fixture.launcher.now, Matches: retromruntime.MatchesCapability,
		PublicOrigin: fixture.launcher.publicOrigin,
		SignIsolation: func(id string) (launchmodel.IsolationTicket, error) {
			origin, ticket, hash, err := fixture.launcher.isolatedRuntimeTicket(id)
			return launchmodel.IsolationTicket{Origin: origin, Ticket: ticket, Hash: hash}, err
		},
	})
}

func assertNoConfig(t *testing.T, configuration Config, err, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("config cause=%v want=%v", err, cause)
	}
	if _, err := configuration.MarshalJSON(); !errors.Is(err, runtimelaunch.ErrEnvelopeInvalid) {
		t.Fatalf("failed issuance returned an envelope: %v", err)
	}
}

func TestConfigSnapshotRetainsSourceAuthority(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			snapshot, found, err := persistence.NewConfig(fixture.database).Load(
				t.Context(), launchmodel.SessionRef{ID: created.LaunchID, Preview: preview}, func(launchmodel.ConfigSource) error { return nil },
			)
			if err != nil || !found {
				t.Fatalf("snapshot found=%v error=%v", found, err)
			}
			source := snapshot.Authority.Source
			if source.State != "CREATED" || source.Version != 1 || !retromruntime.MatchesCapability(
				created.Capability,
				source.CredentialHash,
			) {
				t.Fatalf("invalid frozen authority: state=%s version=%d", source.State, source.Version)
			}
			target, exists := fixture.launcher.runtimeBuilder.Target(source.ProviderID, source.TargetID)
			if !exists || len(snapshot.Files) == 0 {
				t.Fatalf("target=%v files=%d", exists, len(snapshot.Files))
			}
			if len(target.Inputs) == 0 || snapshot.Files[0].Role != "GAME" {
				t.Fatal("snapshot lost Provider game input")
			}
			if err := configDraftFetch(t, fixture, created, preview); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfigFinishDuringEnvelopeBuildPreventsIssuance(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			var afterFinish map[string]string
			builder := configBuildHook{ConfigBuilder: fixture.launcher.runtimeBuilder, after: func() {
				result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "finish",
					PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()})
				if err != nil || result.State != "FINISHED" {
					t.Fatalf("concurrent finish: state=%s error=%v", result.State, err)
				}
				afterFinish = playRows(t, fixture.database)
			}}
			issuer := fixtureConfigIssuer(fixture, persistence.NewConfig(fixture.database), builder)
			configuration, err := issuer.Issue(
				t.Context(), launchmodel.SessionRef{ID: created.LaunchID, Preview: preview}, created.Capability,
			)
			assertNoConfig(t, configuration, err, ErrCredential)
			configDraftUnchanged(t, fixture, afterFinish)
		})
	}
}

func TestConfigActivationFailureRollsBackAllOwners(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			before := playRows(t, fixture.database)
			cause := errors.New("config commit unavailable")
			repository := configRollbackRepository{ConfigRepository: persistence.NewConfig(fixture.database), cause: cause}
			issuer := fixtureConfigIssuer(fixture, repository, fixture.launcher.runtimeBuilder)
			configuration, err := issuer.Issue(
				t.Context(), launchmodel.SessionRef{ID: created.LaunchID, Preview: preview}, created.Capability,
			)
			assertNoConfig(t, configuration, err, cause)
			configDraftUnchanged(t, fixture, before)
		})
	}
}

func TestConfigConcurrentActivationIsIdempotent(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			var activated map[string]string
			builder := configBuildHook{ConfigBuilder: fixture.launcher.runtimeBuilder, after: func() {
				if err := configDraftFetch(t, fixture, created, preview); err != nil {
					t.Fatal(err)
				}
				activated = playRows(t, fixture.database)
			}}
			issuer := fixtureConfigIssuer(fixture, persistence.NewConfig(fixture.database), builder)
			configuration, err := issuer.Issue(
				t.Context(), launchmodel.SessionRef{ID: created.LaunchID, Preview: preview}, created.Capability,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := configuration.MarshalJSON(); err != nil {
				t.Fatal(err)
			}
			configDraftUnchanged(t, fixture, activated)
			if err := configDraftFetch(t, fixture, created, preview); err != nil {
				t.Fatal(err)
			}
			configDraftUnchanged(t, fixture, activated)
		})
	}
}

func TestConfigCancellationAfterBuildDoesNotActivate(t *testing.T) {
	t.Parallel()
	fixture, created := newPlaySourceFixture(t, false, false)
	before := playRows(t, fixture.database)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	builder := configBuildHook{ConfigBuilder: fixture.launcher.runtimeBuilder, after: cancel}
	issuer := fixtureConfigIssuer(fixture, persistence.NewConfig(fixture.database), builder)
	configuration, err := issuer.Issue(ctx, launchmodel.SessionRef{ID: created.LaunchID}, created.Capability)
	assertNoConfig(t, configuration, err, context.Canceled)
	configDraftUnchanged(t, fixture, before)
}

func TestConfigRetryDoesNotStartIdleOrPlaytime(t *testing.T) {
	t.Parallel()
	fixture, created := newPlaySourceFixture(t, false, true)
	before := playRows(t, fixture.database)
	if err := configDraftFetch(t, fixture, created, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, playRows(t, fixture.database)) {
		t.Fatal("config retry changed play/activation state")
	}
	var idle *int64
	var plays int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT idle_expires_at_ms FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(

		&idle,
	); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM play_sessions WHERE launch_session_id=?`, created.LaunchID).Scan(

		&plays,
	); err != nil {
		t.Fatal(err)
	}
	if idle != nil || plays != 0 {
		t.Fatal("config created an idle deadline or playtime")
	}
}
