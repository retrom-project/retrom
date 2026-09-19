package netplay

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	netplaymodel "retrom/internal/model/netplay"
	repository "retrom/internal/repo/netplay"
	application "retrom/internal/service/netplay"
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
		_, err = fixture.database.ExecContext(t.Context(), `UPDATE netplay_session_participants SET state='LAUNCH_READY',launch_session_id=?,credential_sha256=?,credential_generation=1 WHERE netplay_session_id=? AND profile_id=?`, launchID, credential[:], sessionID, profileID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE netplay_sessions SET state=? WHERE id=?`, state, sessionID); err != nil {
		t.Fatal(err)
	}
	return fixture, netplaymodel.PeerIdentity{RoomID: room.RoomID, SessionID: sessionID, ProfileID: "guest", PlayerNo: 2, CredentialGeneration: 1}
}

type failedSessionControl struct {
	repository netplaymodel.SessionControlRepository
	failure    error
	stale      string
}

func (failed failedSessionControl) WithControl(ctx context.Context, work func(netplaymodel.SessionControlScope) error) error {
	return failed.repository.WithControl(ctx, func(scope netplaymodel.SessionControlScope) error {
		scope.Write = staleSessionControlWriter{scope.Write, failed.stale}
		if err := work(scope); err != nil {
			return err
		}
		return failed.failure
	})
}

type staleSessionControlWriter struct {
	netplaymodel.SessionControlWriter

	stale string
}

func (writer staleSessionControlWriter) Session(ctx context.Context, plan netplaymodel.SessionTransitionPlan) error {
	if writer.stale == "session" {
		plan.Before.Version++
	}
	return writer.SessionControlWriter.Session(ctx, plan)
}

func (writer staleSessionControlWriter) Peer(ctx context.Context, plan netplaymodel.PeerTransitionPlan) error {
	if writer.stale == "peer" {
		plan.Peer.CredentialGeneration++
	}
	return writer.SessionControlWriter.Peer(ctx, plan)
}

func TestSessionControlRollsBackVersionsLeasesAndEvents(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"pause", "resync", "run", "ready", "disconnect", "stale session", "stale peer"} {
		t.Run(action, func(t *testing.T) { assertSessionControlRollback(t, action) })
	}
}

func assertSessionControlRollback(t *testing.T, action string) {
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
	stale := ""
	switch action {
	case "stale session":
		stale = "session"
	case "stale peer":
		stale = "peer"
	}
	service := application.NewSessionControl(failedSessionControl{repository.NewSessionControl(fixture.database), sentinel, stale}, 10*time.Second, func() time.Time { return fixture.now })
	var err error
	switch action {
	case "pause", "stale session":
		err = service.SetState(t.Context(), peer.RoomID, peer.SessionID, "host", "PAUSED_RECONNECT")
	case "resync":
		err = service.Resync(t.Context(), peer.RoomID, peer.SessionID, netplaymodel.ResyncHash)
	case "run":
		err = service.Running(t.Context(), peer.RoomID, peer.SessionID)
	case "ready":
		var ready bool
		ready, err = service.RuntimeReady(t.Context(), peer)
		if ready {
			t.Fatal("failed ready publishes success")
		}
	default:
		err = service.Disconnected(t.Context(), peer)
	}
	want := sentinel
	if stale != "" {
		want = ErrRoomConflict
	}
	if !errors.Is(err, want) {
		t.Fatalf("failed transition=%v", err)
	}
	after := sessionControlRecordsSnapshot(t, fixture)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback before=%v after=%v", before, after)
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
	service := application.NewSessionControl(repository.NewSessionControl(fixture.database), 10*time.Second, func() time.Time { return fixture.now })
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
