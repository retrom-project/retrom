//go:build integration

package launch

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/testsupport"
)

func TestNetplayCreationFreezesParticipantLaunchAndReplays(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	created, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if err != nil || created.LaunchID == "" || created.Existing {
		t.Fatalf("create=%q existing=%t error=%v", created.LaunchID, created.Existing, err)
	}
	assertNetplayLaunchCount(t, fixture, 1)
	repeated, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if err != nil || !repeated.Existing || repeated.LaunchID != created.LaunchID || repeated.Capability != created.Capability {
		t.Fatalf("replay=%q existing=%t error=%v", repeated.LaunchID, repeated.Existing, err)
	}
	assertNetplayLaunchBinding(t, fixture, created)
	config, err := fixture.service.Config(t.Context(), created.LaunchID, created.Capability)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testsupport.RuntimeEnvelope(t, config)
	session := testsupport.RuntimeEnvelopeObject(t, envelope, "session")
	if session["purpose"] != "PRODUCT" || session["mode"] != "NETPLAY" {
		t.Fatalf("session=%v", session)
	}
}

func assertNetplayLaunchBinding(t *testing.T, fixture netplayLaunchFixture, created Created) {
	t.Helper()
	var player, generation, files, saves int
	var profileID, sessionID, state, access, bound string
	err := fixture.database.QueryRowContext(
		t.Context(),
		`SELECT launch.profile_id,launch.netplay_session_id,launch.netplay_player_no,
launch.save_access,participant.state,participant.credential_generation,participant.launch_session_id,
(SELECT count(*) FROM launch_content_files WHERE launch_session_id=launch.id),
(SELECT count(*) FROM launch_sessions WHERE id=launch.id AND (save_state_id IS NOT NULL OR dos_entry_path IS NOT NULL))
FROM launch_sessions launch JOIN netplay_session_participants participant ON participant.launch_session_id=launch.id
WHERE launch.id=?`,
		created.LaunchID,
	).Scan(
		&profileID,
		&sessionID,
		&player,
		&access,
		&state,
		&generation,
		&bound,
		&files,
		&saves,
	)
	if err != nil {
		t.Fatal(err)
	}
	if profileID != fixture.request.ProfileID || sessionID != fixture.request.SessionID || player != 1 || generation != 1 ||
		state != "LAUNCH_READY" || access != "NETPLAY_DISABLED" || bound != created.LaunchID || files != 1 || saves != 0 {
		t.Fatalf(
			"binding profile=%s player=%d generation=%d state=%s access=%s files=%d saves=%d",
			profileID,
			player,
			generation,
			state,
			access,
			files,
			saves,
		)
	}
	if created.BootstrapExpiresAtMS != fixture.now().Add(5*time.Minute).UnixMilli() || created.HardExpiresAtMS != fixture.now().Add(8*time.Hour).UnixMilli() {
		t.Fatal("netplay expiration policy changed")
	}
}

func TestNetplayCreationConcurrentParticipantReturnsOneLaunch(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	var clockCalls atomic.Int32
	bothPrepared := make(chan struct{})
	fixture.service.now = func() time.Time {
		number := clockCalls.Add(1)
		if number == 2 {
			close(bothPrepared)
		}
		if number <= 2 {
			<-bothPrepared
		}
		return fixture.now()
	}
	type outcome struct {
		created Created
		err     error
	}
	outcomes := make(chan outcome, 2)
	for range 2 {
		go func() {
			created, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
			outcomes <- outcome{created, err}
		}()
	}
	first, second := <-outcomes, <-outcomes
	if first.err != nil || second.err != nil || first.created.LaunchID == "" || first.created.LaunchID != second.created.LaunchID ||
		first.created.Capability != second.created.Capability || first.created.Existing == second.created.Existing {
		t.Fatalf(
			"concurrent results first=%q/%t/%v second=%q/%t/%v",
			first.created.LaunchID,
			first.created.Existing,
			first.err,
			second.created.LaunchID,
			second.created.Existing,
			second.err,
		)
	}
	assertNetplayLaunchCount(t, fixture, 1)
}

func TestNetplayCreationExistingLifetimePreservesBootstrapAndRejectsHardExpiry(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	created, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.now = func() time.Time { return fixture.now().Add(6 * time.Minute) }
	repeated, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if err != nil || !repeated.Existing || repeated.LaunchID != created.LaunchID || repeated.BootstrapExpiresAtMS != created.BootstrapExpiresAtMS {
		t.Fatalf("bootstrap replay changed: id=%q existing=%t error=%v", repeated.LaunchID, repeated.Existing, err)
	}
	fixture.service.now = func() time.Time { return fixture.now().Add(8 * time.Hour) }
	denied, err := fixture.service.CreateNetplay(t.Context(), fixture.request)
	if !errors.Is(err, ErrBlocked) || denied.LaunchID != "" {
		t.Fatalf("expired replay accepted: %v", err)
	}
	assertNetplayLaunchCount(t, fixture, 1)
}
