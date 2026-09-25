//go:build integration

package launch

import (
	"testing"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

func TestPlaySnapshotsSurviveLostAndReorderedReportsWithoutChangingLaunch(t *testing.T) {
	fixture, created := newProductPlayFixture(t, true)
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE launch_sessions SET idle_expires_at_ms=? WHERE id=?`,
		fixture.now.UnixMilli()-1, created.LaunchID); err != nil {
		t.Fatal(err)
	}
	controller := application.NewPlayController(persistence.NewPlay(fixture.database), fixture.launcher.now, retromruntime.MatchesCapability)
	for _, elapsed := range []int64{30_000, 10_000, 45_000, 45_000} {
		result, err := controller.RecordSnapshot(t.Context(), created.LaunchID, created.Capability,
			application.PlaySnapshot{ActiveDurationMS: elapsed})
		if err != nil || result.ActiveDurationMS != max(30_000, elapsed) {
			t.Fatalf("elapsed=%d result=%#v error=%v", elapsed, result, err)
		}
	}
	var duration, events, due, hard int64
	var state string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT
(SELECT active_duration_ms FROM play_sessions WHERE launch_session_id=launch.id),
(SELECT count(*) FROM play_session_events event JOIN play_sessions play ON play.id=event.play_session_id WHERE play.launch_session_id=launch.id),
(SELECT due_at_ms FROM launch_payload_retirements WHERE launch_session_id=launch.id),
hard_expires_at_ms,state FROM launch_sessions launch WHERE id=?`, created.LaunchID).
		Scan(&duration, &events, &due, &hard, &state); err != nil {
		t.Fatal(err)
	}
	if duration != 45_000 || events != 0 || due != hard || state != "ACTIVE" {
		t.Fatalf("duration=%d events=%d due=%d hard=%d state=%s", duration, events, due, hard, state)
	}
	if _, err := fixture.launcher.Config(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatalf("expired idle deadline blocked config: %v", err)
	}
	if err := fixture.launcher.AuthorizeSave(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatalf("expired idle deadline blocked save: %v", err)
	}
	var logicalName string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT logical_name FROM launch_content_files
WHERE launch_session_id=? LIMIT 1`, created.LaunchID).Scan(&logicalName); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.launcher.ContentAuthorized(t.Context(), created.LaunchID, logicalName, false); err != nil {
		t.Fatalf("expired idle deadline blocked content: %v", err)
	}
}
