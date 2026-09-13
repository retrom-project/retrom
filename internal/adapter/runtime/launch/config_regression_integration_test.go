//go:build integration

package launch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"retrom/internal/capability/runtime/runtimelaunch"
	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

func configDraftFetch(t *testing.T, fixture reviewCheckpointFixture, created Created, preview bool) error {
	t.Helper()
	var err error
	if preview {
		_, err = fixture.launcher.ReviewPreviewConfig(t.Context(), created.LaunchID, created.Capability)
	} else {
		_, err = fixture.launcher.Config(t.Context(), created.LaunchID, created.Capability)
	}
	return err
}

func configDraftSourceTable(preview bool) string {
	if preview {
		return "review_preview_sessions"
	}
	return "launch_sessions"
}

func configDraftUnchanged(t *testing.T, fixture reviewCheckpointFixture, before map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(playRows(t, fixture.database), before) {
		t.Fatal("failed config changed lifecycle, capability, save, or retirement rows")
	}
}

func TestConfigUnavailableTargetDoesNotActivate(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			before := playRows(t, fixture.database)
			// A non-nil, empty in-memory Builder models an unavailable frozen
			// Target. Creation used the real Provider builder and real DB schema.
			fixture.launcher.runtimeBuilder = &runtimelaunch.Builder{}
			if err := configDraftFetch(t, fixture, created, preview); !errors.Is(err, ErrCredential) {
				t.Fatalf("unavailable Target error=%v", err)
			}
			configDraftUnchanged(t, fixture, before)
		})
	}
}

func TestConfigRechecksBootstrapAtActivation(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			var bootstrapEnd int64
			query := "SELECT bootstrap_expires_at_ms FROM " + configDraftSourceTable(preview) + " WHERE id=?"
			if err := fixture.database.QueryRowContext(t.Context(), query, created.LaunchID).Scan(&bootstrapEnd); err != nil {
				t.Fatal(err)
			}
			before := playRows(t, fixture.database)
			initial := fixture.launcher.now()
			calls := 0
			fixture.launcher.now = func() time.Time {
				calls++
				if calls == 1 {
					return initial
				}
				return time.UnixMilli(bootstrapEnd)
			}
			if err := configDraftFetch(t, fixture, created, preview); !errors.Is(err, ErrCredential) {
				t.Errorf("config crossed bootstrap boundary: %v", err)
			}
			configDraftUnchanged(t, fixture, before)
		})
	}
}

func TestConfigActivationRejectsAlreadyFinishedSource(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			// Capture exactly the state field passed by today's public Config.
			var staleVersion int64
			query := "SELECT version FROM " + configDraftSourceTable(preview) + " WHERE id=?"
			if err := fixture.database.QueryRowContext(t.Context(), query, created.LaunchID).Scan(&staleVersion); err != nil {
				t.Fatal(err)
			}
			result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability,
				"finish", PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()})
			if err != nil || result.State != "FINISHED" {
				t.Fatalf("fixture finish state=%s error=%v", result.State, err)
			}
			before := playRows(t, fixture.database)
			err = persistence.NewConfig(fixture.database).WithActivation(
				t.Context(),
				func(transaction application.ConfigActivation) error {
					return transaction.Activate(t.Context(), application.ConfigActivationPlan{
						Ref: application.SessionRef{
							ID:      created.LaunchID,
							Preview: preview,
						}, Version: staleVersion, NowMS: fixture.now.UnixMilli(),
					})
				},
			)
			if !errors.Is(err, ErrCredential) {
				t.Errorf("stale activation accepted finished session: %v", err)
			}
			configDraftUnchanged(t, fixture, before)
		})
	}
}

func TestConfigPreservesCanceledSourceRead(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			before := playRows(t, fixture.database)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var err error
			if preview {
				_, err = fixture.launcher.ReviewPreviewConfig(ctx, created.LaunchID, created.Capability)
			} else {
				_, err = fixture.launcher.Config(ctx, created.LaunchID, created.Capability)
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("config source cancellation cause lost: %v", err)
			}
			configDraftUnchanged(t, fixture, before)
		})
	}
}
