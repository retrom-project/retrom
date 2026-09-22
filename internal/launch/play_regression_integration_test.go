//go:build integration

package launch

import (
	"errors"
	"testing"

	"retrom/internal/libraryimport"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

func newProductPlayFixture(t *testing.T, activate bool) (reviewCheckpointFixture, Created) {
	t.Helper()
	fixture := newReviewCheckpointFixture(t)
	mustRPGLaunchSQL(t, fixture.database, `UPDATE review_drafts SET metadata_json='{"title":"Play lifecycle"}' WHERE import_item_id=?`, fixture.itemID)
	importer := libraryimport.New(fixture.database, fixture.launcher.now)
	published, err := importer.Approve(t.Context(), fixture.itemID, 2)
	if err != nil {
		t.Fatal(err)
	}
	created, err := fixture.launcher.Create(t.Context(), "local", CreateRequest{GameID: published.GameID, ReturnTo: "/games/" + published.GameID})
	if err != nil {
		t.Fatal(err)
	}
	if activate {
		if _, err := fixture.launcher.Config(t.Context(), created.LaunchID, created.Capability); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, created
}

func TestRecordPlayFinishDuringLoadingClosesCreatedLaunch(t *testing.T) {
	t.Parallel()
	fixture, created := newProductPlayFixture(t, false)
	event := PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()}
	result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "finish", event)
	if err != nil || result.State != "FINISHED" || result.PlaySessionID != nil || result.AcceptedDuration != 0 {
		t.Fatalf("finish=%#v error=%v", result, err)
	}
	var state string
	var playCount int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM play_sessions WHERE launch_session_id=?`, created.LaunchID).Scan(&playCount); err != nil {
		t.Fatal(err)
	}
	if state != "FINISHED" || playCount != 0 {
		t.Fatalf("loading finish left state=%s play sessions=%d", state, playCount)
	}
	if err := fixture.launcher.AuthorizeSave(t.Context(), created.LaunchID, created.Capability); !errors.Is(err, ErrCredential) {
		t.Fatalf("finished capability remains usable: %v", err)
	}
}

type playEntropyFailure struct{ cause error }

func (failure playEntropyFailure) Read([]byte) (int, error) { return 0, failure.cause }

func TestRecordPlayStartRejectsIdentityFailureAtomically(t *testing.T) {
	fixture, created := newProductPlayFixture(t, true)
	cause := errors.New("play identity entropy unavailable")
	result, err := func() (PlayResult, error) {
		uuid.SetRand(playEntropyFailure{cause: cause})
		defer uuid.SetRand(nil)
		return fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "start", PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()})
	}()
	if !errors.Is(err, cause) || result.PlaySessionID != nil {
		t.Fatalf("entropy failure started play: %#v %v", result, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM play_sessions WHERE launch_session_id=?`, created.LaunchID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("entropy failure committed %d play sessions", count)
	}
}

func TestRecordPlayRejectsUnknownKindWhenReplayingHeartbeat(t *testing.T) {
	t.Parallel()
	fixture, created := newProductPlayFixture(t, true)
	start := PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()}
	if _, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "start", start); err != nil {
		t.Fatal(err)
	}
	heartbeat := PlayEvent{ClientSequence: 1, ClientObservedAtMS: fixture.now.UnixMilli(), PreviousInterval: &Interval{Running: true, Visible: true}}
	if _, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "heartbeat", heartbeat); err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "unknown", heartbeat); !errors.Is(err, ErrBlocked) || result.PlaySessionID != nil {
		t.Fatalf("unknown replay kind accepted: %#v %v", result, err)
	}
}

func TestRecordPlayDoesNotReplayRevokedSession(t *testing.T) {
	t.Parallel()
	fixture, created := newProductPlayFixture(t, true)
	event := PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()}
	if _, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "start", event); err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, fixture.database, `UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,version=version+1 WHERE id=?`, fixture.now.UnixMilli(), created.LaunchID)
	if result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "start", event); !errors.Is(err, ErrCredential) || result.PlaySessionID != nil {
		t.Fatalf("revoked session replayed: %#v %v", result, err)
	}
}

func TestRecordPlayPreservesSourceStorageError(t *testing.T) {
	t.Parallel()
	fixture, created := newProductPlayFixture(t, true)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE launch_sessions RENAME TO unavailable_launch_sessions`)
	result, err := fixture.launcher.RecordPlay(t.Context(), created.LaunchID, created.Capability, "start", PlayEvent{ClientObservedAtMS: fixture.now.UnixMilli()})
	var storageError *sqlite.Error
	if !errors.As(err, &storageError) || result.PlaySessionID != nil {
		t.Fatalf("source storage cause lost: %#v %v", result, err)
	}
}
