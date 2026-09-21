//go:build integration

package launch

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/netplay/capability"
	netplaypersistence "retrom/internal/persistence/netplay"
	netplayservice "retrom/internal/service/netplay"
	"retrom/internal/testsupport"
)

func netplayPreparationFixture(t *testing.T, fixture netplayLaunchFixture) *netplayservice.ParticipantPreparation {
	t.Helper()
	signer, err := capability.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exit := netplayservice.NewRoomExit(netplaypersistence.NewRoomExit(fixture.database), time.Hour, fixture.now)
	return netplayservice.NewParticipantPreparation(
		netplaypersistence.NewParticipantPreparation(fixture.database),
		signer,
		exit,
		fixture.now,
	)
}

func TestNetplayPreparationConsumerRecordsEachParticipantOnce(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	service := netplayPreparationFixture(t, fixture)
	request := netplayservice.PreparationRequest{
		RoomID: fixture.request.RoomID, SessionID: fixture.request.SessionID,
		ProfileID: fixture.request.ProfileID, Capabilities: fixture.request.ClientCapabilities,
	}
	host, err := service.Launch(t.Context(), fixture.service, request)
	if err != nil || host.Launch.LaunchID == "" || host.RoomCapability == "" {
		t.Fatalf("prepare host error=%v", err)
	}
	repeated, err := service.Launch(t.Context(), fixture.service, request)
	if err != nil || !repeated.Launch.Existing || repeated.Launch.LaunchID != host.Launch.LaunchID || repeated.RoomCapability != host.RoomCapability {
		t.Fatalf("repeat host error=%v", err)
	}
	request.ProfileID = netplayGuestProfile
	guest, err := service.Launch(t.Context(), fixture.service, request)
	if err != nil || guest.Launch.LaunchID == "" || guest.Launch.LaunchID == host.Launch.LaunchID {
		t.Fatalf("prepare guest error=%v", err)
	}
	var state string
	var participants, loading int
	err = fixture.database.QueryRowContext(t.Context(), `SELECT state,
(SELECT count(*) FROM netplay_events WHERE netplay_session_id=session.id AND event_type='PARTICIPANT_STATE_CHANGED'),
(SELECT count(*) FROM netplay_events WHERE netplay_session_id=session.id AND event_type='SESSION_STATE_CHANGED' AND json_extract(data_json,'$.toState')='LOADING')
FROM netplay_sessions session WHERE id=?`, request.SessionID).Scan(&state, &participants, &loading)
	if err != nil || state != "LOADING" || participants != 2 || loading != 1 {
		t.Fatalf("preparation state=%s events=%d/%d error=%v", state, participants, loading, err)
	}
	assertNetplayLaunchCount(t, fixture, 2)
}

func TestNetplayPreparationConsumerRetainsSeparateCreationAndAbort(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	cause := errors.New("participant preparation event unavailable")
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(
				strings.TrimSpace(query),
				"INSERT INTO netplay_events(",
			) && len(
				args,
			) == 7 && args[1].Value == fixture.request.SessionID && args[4].Value == "PARTICIPANT_STATE_CHANGED" {
				hits++
				return cause
			}
			return nil
		},
	})
	signer, err := capability.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exit := netplayservice.NewRoomExit(netplaypersistence.NewRoomExit(fixture.database), time.Hour, fixture.now)
	service := netplayservice.NewParticipantPreparation(
		netplaypersistence.NewParticipantPreparation(fault),
		signer,
		exit,
		fixture.now,
	)
	result, err := service.Launch(t.Context(), fixture.service, netplayservice.PreparationRequest{
		RoomID: fixture.request.RoomID, SessionID: fixture.request.SessionID, ProfileID: fixture.request.ProfileID, Capabilities: fixture.request.ClientCapabilities,
	})
	if hits != 1 || !errors.Is(err, cause) || result.Launch.LaunchID != "" {
		t.Fatalf("prepare fault hits=%d error=%v", hits, err)
	}
	assertNetplayPreparationAborted(t, fixture, 1)
}

func TestNetplayPreparationConsumerAbortsAfterRequestCancellation(t *testing.T) {
	fixture := newNetplayLaunchFixture(t)
	service := netplayPreparationFixture(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.service.now = func() time.Time { cancel(); return fixture.now() }
	result, err := service.Launch(ctx, fixture.service, netplayservice.PreparationRequest{
		RoomID: fixture.request.RoomID, SessionID: fixture.request.SessionID, ProfileID: fixture.request.ProfileID, Capabilities: fixture.request.ClientCapabilities,
	})
	if !errors.Is(err, context.Canceled) || result.Launch.LaunchID != "" {
		t.Fatalf("cancel preparation error=%v", err)
	}
	assertNetplayPreparationAborted(t, fixture, 0)
}

func assertNetplayPreparationAborted(t *testing.T, fixture netplayLaunchFixture, wantLaunches int) {
	t.Helper()
	var roomState, sessionState, reason string
	var activeLaunches, left, ready int
	err := fixture.database.QueryRowContext(
		t.Context(),
		`SELECT room.state,session.state,session.end_reason,
(SELECT count(*) FROM launch_sessions WHERE netplay_session_id=session.id AND state IN ('CREATED','ACTIVE')),
(SELECT count(*) FROM netplay_session_participants WHERE netplay_session_id=session.id AND state='LEFT'),
(SELECT count(*) FROM netplay_room_members WHERE room_id=room.id AND ready=1)
FROM netplay_sessions session JOIN netplay_rooms room ON room.id=session.room_id WHERE session.id=?`,
		fixture.request.SessionID,
	).
		Scan(&roomState, &sessionState, &reason, &activeLaunches, &left, &ready)
	if err != nil || roomState != "WAITING" || sessionState != "FAILED" || reason != "PREPARE_FAILED" || activeLaunches != 0 || left != 2 || ready != 0 {
		t.Fatalf(
			"abort room=%s session=%s/%s active=%d left=%d ready=%d error=%v",
			roomState,
			sessionState,
			reason,
			activeLaunches,
			left,
			ready,
			err,
		)
	}
	assertNetplayLaunchCount(t, fixture, wantLaunches)
}
