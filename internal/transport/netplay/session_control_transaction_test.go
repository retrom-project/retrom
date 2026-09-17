package netplay

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	netplaymodel "retrom/internal/model/netplay"
	repository "retrom/internal/repo/netplay"
	netplayservice "retrom/internal/service/netplay"
	"retrom/internal/testkit/testsupport"
)

func controlledSessionFixture(t *testing.T, state string) (controlFixture, netplaymodel.PeerIdentity) {
	t.Helper()
	fixture := readyControlFixture(t)
	room, err := fixture.service.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
	if err != nil {
		t.Fatal(err)
	}
	fixture.room = room
	sessionID := room.CurrentSession.SessionID
	for _, profileID := range []string{"host", "guest"} {
		launchID := "launch-" + profileID
		credential := sha256.Sum256([]byte(profileID))
		_, err := fixture.database.ExecContext(t.Context(), `
INSERT INTO launch_sessions(id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,
content_kind,dependency_snapshot_json,compatibility_code,return_to,credential_sha256,state,
bootstrap_expires_at_ms,activated_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,
netplay_session_id,netplay_player_no,save_access)
SELECT ?,?,session.game_id,'fceumm',session.provider_id,session.target_id,session.bundle_sha256,
'SINGLE_FILE','{"schemaVersion":1,"kind":"STATIC","bios":[]}','READY','/netplay/rooms/'||session.room_id,?,'ACTIVE',
?,?,?,?,?,session.id,participant.player_no,'NETPLAY_DISABLED'
FROM netplay_sessions session JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
WHERE session.id=? AND participant.profile_id=?`, launchID, profileID, credential[:], fixture.now.Add(time.Minute).UnixMilli(), fixture.now.UnixMilli(), fixture.now.Add(time.Hour).UnixMilli(), fixture.now.UnixMilli(), fixture.now.UnixMilli(), sessionID, profileID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = fixture.database.ExecContext(t.Context(), `UPDATE netplay_session_participants SET state=? ,launch_session_id=?,credential_sha256=?,credential_generation=1 WHERE netplay_session_id=? AND profile_id=?`, "LAUNCH_READY", launchID, credential[:], sessionID, profileID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_sessions SET state=? WHERE id=?`, state, sessionID); err != nil {
		t.Fatal(err)
	}
	return fixture, netplaymodel.PeerIdentity{RoomID: room.RoomID, SessionID: sessionID, ProfileID: "guest", PlayerNo: 2, CredentialGeneration: 1}
}

func TestSessionControlRollsBackVersionsLeasesAndEvents(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"pause", "resync", "run", "ready", "disconnect", "stale session", "stale peer"} {
		t.Run(action, func(t *testing.T) { assertSessionControlRollback(t, action) })
	}
}

func assertSessionControlRollback(t *testing.T, action string) {
	t.Helper()
	switch action {
	case "stale session":
		assertStaleSessionRollback(t)
	case "stale peer":
		assertStalePeerRollback(t)
	default:
		assertFaultInjectionRollback(t, action)
	}
}

func assertStaleSessionRollback(t *testing.T) {
	t.Helper()
	fixture, peer := controlledSessionFixture(t, "RUNNING")
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_session_participants SET state='CONNECTED' WHERE netplay_session_id=?`, peer.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_sessions SET state='LOADING' WHERE id=?`, peer.SessionID); err != nil {
		t.Fatal(err)
	}
	before := sessionControlRecordsSnapshot(t, fixture)
	service := netplayservice.NewSessionControl(repository.NewSessionControl(fixture.database), 10*time.Second, func() time.Time { return fixture.now })
	if err := service.SetState(t.Context(), peer.RoomID, peer.SessionID, "host", "PAUSED_RECONNECT"); err == nil {
		t.Fatal("stale session: expected error, got nil")
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("stale session changed records")
	}
}

func assertStalePeerRollback(t *testing.T) {
	t.Helper()
	fixture, peer := controlledSessionFixture(t, "RUNNING")
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_session_participants SET state='CONNECTED' WHERE netplay_session_id=?`, peer.SessionID); err != nil {
		t.Fatal(err)
	}
	before := sessionControlRecordsSnapshot(t, fixture)
	stalePeer := peer
	stalePeer.CredentialGeneration = 999
	service := netplayservice.NewSessionControl(repository.NewSessionControl(fixture.database), 10*time.Second, func() time.Time { return fixture.now })
	if err := service.Disconnected(t.Context(), stalePeer); err == nil {
		t.Fatal("stale peer: expected error, got nil")
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("stale peer changed records")
	}
}

func assertFaultInjectionRollback(t *testing.T, action string) {
	t.Helper()
	state := "RUNNING"
	peerState := "CONNECTED"
	switch action {
	case "run":
		state = "RESYNCHRONIZING"
		peerState = "RUNTIME_READY"
	case "ready":
		state = "LOADING"
		peerState = "LAUNCH_READY"
	}
	fixture, peer := controlledSessionFixture(t, state)
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_session_participants SET state=? WHERE netplay_session_id=?`, peerState, peer.SessionID); err != nil {
		t.Fatal(err)
	}
	before := sessionControlRecordsSnapshot(t, fixture)
	sentinel := errors.New("late transition failure")
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			q := strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
			if strings.HasPrefix(q, "INSERT INTO netplay_events(") {
				hits.Add(1)
				return sentinel
			}
			return nil
		},
	})
	service := netplayservice.NewSessionControl(repository.NewSessionControl(faultDB), 10*time.Second, func() time.Time { return fixture.now })
	err := executeSessionAction(t, service, action, peer)
	if !errors.Is(err, sentinel) {
		t.Fatalf("failed transition=%v", err)
	}
	if hits.Load() < 1 {
		t.Fatal("fault injection did not fire")
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback before=%v after=%v", before, after)
	}
}

func executeSessionAction(t *testing.T, service *netplayservice.SessionControl, action string, peer netplaymodel.PeerIdentity) error {
	t.Helper()
	switch action {
	case "pause":
		return service.SetState(t.Context(), peer.RoomID, peer.SessionID, "host", "PAUSED_RECONNECT")
	case "resync":
		return service.Resync(t.Context(), peer.RoomID, peer.SessionID, netplaymodel.ResyncHash)
	case "run":
		return service.Running(t.Context(), peer.RoomID, peer.SessionID)
	case "ready":
		ready, err := service.RuntimeReady(t.Context(), peer)
		if ready {
			t.Fatal("failed ready publishes success")
		}
		return err
	default:
		return service.Disconnected(t.Context(), peer)
	}
}

func sessionControlRecordsSnapshot(t *testing.T, fixture controlFixture) []string {
	t.Helper()
	result := roomExitRecordsSnapshot(t, fixture)
	return append(result, readExitRecords(t, fixture, `SELECT json_object('id',id,'resync',resync_count,'start',started_at_ms) FROM netplay_sessions ORDER BY id`)...)
}

func TestSessionControlReadyAndReconnectPersistCoherentState(t *testing.T) {
	t.Parallel()
	fixture, guest := controlledSessionFixture(t, "LOADING")
	service := netplayservice.NewSessionControl(repository.NewSessionControl(fixture.database), 10*time.Second, func() time.Time { return fixture.now })
	host := guest
	host.ProfileID = "host"
	host.PlayerNo = 1
	if ready, err := service.RuntimeReady(t.Context(), host); err != nil || ready {
		t.Fatalf("host ready=%v error=%v", ready, err)
	}
	if ready, err := service.RuntimeReady(t.Context(), guest); err != nil || !ready {
		t.Fatalf("guest ready=%v error=%v", ready, err)
	}
	if err := service.Running(t.Context(), guest.RoomID, guest.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := service.Disconnected(t.Context(), guest); err != nil {
		t.Fatal(err)
	}
	before := sessionControlRecordsSnapshot(t, fixture)
	if err := service.Disconnected(t.Context(), guest); err != nil {
		t.Fatal(err)
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("duplicate disconnect changed records")
	}
	if err := service.Resync(t.Context(), guest.RoomID, guest.SessionID, netplaymodel.ResyncReconnect); err != nil {
		t.Fatal(err)
	}
	if err := service.Running(t.Context(), guest.RoomID, guest.SessionID); err != nil {
		t.Fatal(err)
	}
	assertReconnectedSession(t, fixture, guest.SessionID)
}

func assertReconnectedSession(t *testing.T, fixture controlFixture, sessionID string) {
	t.Helper()
	var state string
	var resync, connected int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state,resync_count FROM netplay_sessions WHERE id=?`, sessionID).Scan(&state, &resync); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM netplay_session_participants WHERE state='CONNECTED' AND lease_expires_at_ms IS NULL AND disconnected_at_ms IS NULL`).Scan(&connected); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || resync != 1 || connected != 2 {
		t.Fatalf("session=%s resync=%d connected=%d", state, resync, connected)
	}
}

func TestStaleSocketGenerationCannotDisconnectReplacement(t *testing.T) {
	t.Parallel()
	fixture, peer := controlledSessionFixture(t, "RUNNING")
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_session_participants SET state='CONNECTED',credential_generation=2 WHERE netplay_session_id=?`, peer.SessionID); err != nil {
		t.Fatal(err)
	}
	before := sessionControlRecordsSnapshot(t, fixture)
	err := fixture.service.MarkDisconnected(t.Context(), SocketParticipant{RoomID: peer.RoomID, SessionID: peer.SessionID, ProfileID: peer.ProfileID, PlayerNo: peer.PlayerNo, CredentialGeneration: peer.CredentialGeneration})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("stale socket disconnect=%v", err)
	}
	if after := sessionControlRecordsSnapshot(t, fixture); !reflect.DeepEqual(before, after) {
		t.Fatal("stale socket changed replacement")
	}
}
