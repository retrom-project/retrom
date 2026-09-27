//go:build integration

package launch

import (
	"errors"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/libraryimport"
	application "retrom/internal/service/launch"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

func newProductPlayFixture(t *testing.T, activate bool) (reviewCheckpointFixture, Created) {
	t.Helper()
	fixture := newReviewCheckpointFixture(t)
	mustRPGLaunchSQL(t, fixture.database, `UPDATE import_items SET metadata_json='{"title":"Play lifecycle"}' WHERE id=?`, fixture.itemID)
	importer := libraryimport.New(fixture.database, fixture.launcher.now).WithFileStore(fixture.files)
	published, err := importer.Approve(t.Context(), fixture.itemID, 2)
	if err != nil {
		t.Fatal(err)
	}
	created, err := fixture.launcher.Create(t.Context(), "local",
		CreateRequest{GameID: published.GameID, ReturnTo: "/games/" + published.GameID})
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

type playEntropyFailure struct{ cause error }

func (failure playEntropyFailure) Read([]byte) (int, error) { return 0, failure.cause }

func TestPlaySnapshotRejectsIdentityFailureAtomically(t *testing.T) {
	fixture, created := newProductPlayFixture(t, true)
	cause := errors.New("play identity entropy unavailable")
	result, err := func() (application.PlaySnapshotResult, error) {
		uuid.SetRand(playEntropyFailure{cause: cause})
		defer uuid.SetRand(nil)
		return fixture.launcher.RecordPlaySnapshot(t.Context(), created.LaunchID, created.Capability, PlaySnapshot{})
	}()
	if !errors.Is(err, cause) || result.PlaySessionID != "" {
		t.Fatalf("entropy failure started play: %#v %v", result, err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT count(*) FROM play_sessions WHERE launch_session_id=?`, created.LaunchID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("entropy failure committed %d play sessions", count)
	}
}

func TestPlaySnapshotRejectsRevokedSession(t *testing.T) {
	t.Parallel()
	fixture, created := newProductPlayFixture(t, true)
	event := PlaySnapshot{ActiveDurationMS: 1000}
	if _, err := fixture.launcher.RecordPlaySnapshot(t.Context(), created.LaunchID, created.Capability,
		event); err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, fixture.database, `UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,version=version+1 WHERE id=?`, fixture.now.UnixMilli(), created.LaunchID)
	if result, err := fixture.launcher.RecordPlaySnapshot(t.Context(), created.LaunchID,
		created.Capability, event); !errors.Is(err, ErrCredential) || result.PlaySessionID != "" {
		t.Fatalf("revoked session replayed: %#v %v", result, err)
	}
}

func TestPlaySnapshotPreservesSourceStorageError(t *testing.T) {
	t.Parallel()
	fixture, created := newProductPlayFixture(t, true)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE launch_sessions RENAME TO unavailable_launch_sessions`)
	result, err := fixture.launcher.RecordPlaySnapshot(t.Context(), created.LaunchID, created.Capability, PlaySnapshot{})
	var storageError *sqlite.Error
	if !errors.As(err, &storageError) || result.PlaySessionID != "" {
		t.Fatalf("source storage cause lost: %#v %v", result, err)
	}
}
