//go:build integration

package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	retromruntime "retrom/internal/adapter/runtime/runtime"
	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

type failPlayAfterWork struct {
	repository application.PlayRepository
	failure    error
}

func (repository failPlayAfterWork) WithPlay(ctx context.Context, work func(application.PlayScope) error) error {
	return repository.repository.WithPlay(ctx, func(scope application.PlayScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return repository.failure
	})
}

func playRows(t *testing.T, database *sql.DB) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{
		"launch_sessions", "play_sessions", "play_session_events", "review_preview_sessions",
		"launch_payload_retirements", "launch_game_save_bindings", "isolated_runtime_capabilities", "isolated_runtime_bootstrap_tickets", "save_states",
	} {
		result[table] = playTableRows(t, database, table)
	}
	return result
}

func playTableRows(t *testing.T, database *sql.DB, table string) string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY 1,2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	records := make([][]any, 0)
	for rows.Next() {
		row := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range row {
			destinations[index] = &row[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		records = append(records, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func productPlayStart(t *testing.T, fixture reviewCheckpointFixture, created Created) {
	t.Helper()
	if _, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "start", PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
}

func seedPlayCapability(t *testing.T, fixture reviewCheckpointFixture, id string, preview bool) {
	t.Helper()
	var launchID, previewID *string
	if preview {
		previewID = &id
	} else {
		launchID = &id
	}
	mustRPGLaunchSQL(t, fixture.database, `INSERT INTO isolated_runtime_capabilities(credential_sha256,launch_id,preview_id,
 profile_id,expected_origin,issued_at_ms,expires_at_ms) VALUES(zeroblob(32),?,?,'local','http://play.localhost:3000',?,?)`,
		launchID, previewID, fixture.now.UnixMilli(), fixture.now.UnixMilli()+1_000_000)
}

func TestPlayTransactionRollsBackEveryLifecycleWrite(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"start", "heartbeat", "finish", "loading-finish", "preview-finish"} {
		t.Run(kind, func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, kind == "preview-finish", kind != "loading-finish")
			event := PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()}
			requestKind := kind
			if kind == "heartbeat" || kind == "finish" {
				productPlayStart(t, fixture, created)
				event.ClientSequence = 1
				event.PreviousInterval = &Interval{Running: true, Visible: true}
			}
			if kind == "loading-finish" {
				requestKind = "finish"
			}
			if kind == "preview-finish" {
				requestKind = "finish"
			}
			seedPlayCapability(t, fixture, created.LaunchID, kind == "preview-finish")
			before := playRows(t, fixture.database)
			cause := errors.New("late play callback failure")
			repository := failPlayAfterWork{repository: persistence.NewPlay(fixture.database), failure: cause}
			controller := application.NewPlayController(repository, fixture.launcher.now, retromruntime.MatchesCapability)
			result, err := controller.RecordPlay(t.Context(), created.LaunchID, created.Capability, requestKind, event)
			if !errors.Is(err, cause) || result.PlaySessionID != nil || result.State != "" {
				t.Fatalf("late failure result=%#v error=%v", result, err)
			}
			if after := playRows(t, fixture.database); !reflect.DeepEqual(before, after) {
				t.Fatal("late failure committed lifecycle/ownership/play changes")
			}
			if fixture.database.Stats().InUse != 0 {
				t.Fatal("play transaction retained a connection")
			}
		})
	}
}

func TestPlayProgressRollsBackWhenFinalSourceFenceIsStale(t *testing.T) {
	t.Parallel()
	for _, stale := range []string{"source-version", "play-version", "sequence", "hard-expiry", "idle-expiry"} {
		t.Run(stale, func(t *testing.T) {
			fixture, created := newProductPlayFixture(t, true)
			productPlayStart(t, fixture, created)
			before := playRows(t, fixture.database)
			err := persistence.NewPlay(fixture.database).WithPlay(t.Context(), func(scope application.PlayScope) error {
				source, found, err := scope.Read.Source(t.Context(), created.LaunchID)
				if err != nil || !found {
					t.Fatalf("source found=%t error=%v", found, err)
				}
				current, found, err := scope.Read.Current(t.Context(), created.LaunchID)
				if err != nil || !found {
					t.Fatalf("play found=%t error=%v", found, err)
				}
				now := fixture.now.UnixMilli()
				switch stale {
				case "source-version":
					source.Version++
				case "play-version":
					current.Version++
				case "sequence":
					current.LastSequence++
				case "hard-expiry":
					now = source.Session.HardExpiresAtMS
				case "idle-expiry":
					now = *source.IdleExpiresAtMS
				}
				return scope.Write.Progress(t.Context(), application.PlayProgress{
					Source: source, Current: current, Kind: "heartbeat",
					Event: application.PlayEvent{ClientSequence: 1, ClientObservedAtMS: now, PreviousInterval: &application.Interval{Running: true, Visible: true}},
					NowMS: now, IdleExpiresAtMS: now + 120_000, AcceptedDurationMS: 20,
				})
			})
			if !errors.Is(err, application.ErrBlocked) {
				t.Fatalf("stale fence error=%v", err)
			}
			if after := playRows(t, fixture.database); !reflect.DeepEqual(before, after) {
				t.Fatal("stale progress partially committed")
			}
		})
	}
}

func TestPlayFinishRevokesCapabilityAndPreservesLoadingIdempotency(t *testing.T) {
	t.Parallel()
	for _, previewMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "product", true: "preview"}[previewMode], func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, previewMode, false)
			seedPlayCapability(t, fixture, created.LaunchID, previewMode)
			event := PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()}
			first, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "finish", event)
			if err != nil || first.PlaySessionID != nil || first.State != "FINISHED" {
				t.Fatalf("first finish=%#v error=%v", first, err)
			}
			before := playRows(t, fixture.database)
			second, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "finish", event)
			if err != nil || first != second || !reflect.DeepEqual(before, playRows(t, fixture.database)) {
				t.Fatalf("repeat finish=%#v error=%v", second, err)
			}
			var revoked int64
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT revoked_at_ms FROM isolated_runtime_capabilities`).Scan(&revoked); err != nil || revoked != fixture.now.UnixMilli() {
				t.Fatalf("revoked=%d error=%v", revoked, err)
			}
			var count int
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM play_sessions`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("loading created play rows=%d error=%v", count, err)
			}
		})
	}
}

func newPlaySourceFixture(t *testing.T, previewMode, activate bool) (reviewCheckpointFixture, Created) {
	t.Helper()
	if !previewMode {
		return newProductPlayFixture(t, activate)
	}
	fixture := newReviewCheckpointFixture(t)
	preview, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "play-preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	if activate {
		if _, err := fixture.launcher.ReviewPreviewConfig(t.Context(), preview.PreviewID, preview.Capability); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, Created{LaunchID: preview.PreviewID, Capability: preview.Capability}
}
