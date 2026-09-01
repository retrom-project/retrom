package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"retrom/internal/testassert"

	"github.com/coder/websocket"
)

func websocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		accepted <- connection
		<-handlerDone
	}))
	client, response, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	serverConnection := <-accepted
	t.Cleanup(func() {
		_ = client.CloseNow()
		_ = serverConnection.CloseNow()
		close(handlerDone)
		server.Close()
	})
	return serverConnection, client
}

func TestPausedSessionIgnoresValidInFlightInputAndHash(t *testing.T) {
	t.Parallel()
	session := &realtimeSession{
		nextFrame: 120,
		inputs:    make(map[int64]map[int][24]int16),
		hashes:    make(map[int64]map[int]string),
	}
	controls := make([]int16, ControlCount)
	if err := session.acceptInput(context.Background(), 2, 127, controls); err != nil {
		t.Fatalf("valid in-flight input at pause boundary: %v", err)
	}
	if err := session.acceptHash(context.Background(), 2, 119, "a"+string(make([]byte, 63))); err == nil {
		t.Fatal("invalid checkpoint digest accepted")
	}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := session.acceptHash(context.Background(), 2, 119, digest); err != nil {
		t.Fatalf("valid in-flight hash at pause boundary: %v", err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(session.inputs) != 0 }, func() bool { return len(session.hashes) != 0 }), "paused in-flight messages mutated session: inputs=%d hashes=%d", len(session.inputs), len(session.hashes))
	if err := session.acceptInput(context.Background(), 2, 241, controls); !errors.Is(err, ErrProtocol) {
		t.Fatalf("out-of-window paused input error = %v, want protocol violation", err)
	}
}

func TestAcceptanceNP010InputRateLimitAllowsBurstAndRefillsAt120PerSecond(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_000_000, 0)
	client := &peer{inputTokens: 240, inputRefill: now}
	for index := 0; index < 240; index++ {
		testassert.Truef(t, client.allowInput(now), "burst contribution %d rejected", index+1)
	}
	testassert.False(t, client.allowInput(now), "241st contribution in one burst was accepted")
	now = now.Add(time.Second/120 + time.Nanosecond)
	testassert.False(t, testassert.Any(func() bool { return !client.allowInput(now) }, func() bool { return client.allowInput(now) }), "rate limiter did not refill exactly one token at 120/s")
}

func TestReconnectDuringInitialTransferRestartsTheBarrier(t *testing.T) {
	ctx := context.Background()
	session := &realtimeSession{
		service: &Service{}, sessionID: "session", profileDigest: "profile", coreArtifact: "core",
		occupiedMask: 3, playerCount: 2, peers: map[int]*peer{
			1: {
				participant: SocketParticipant{PlayerNo: 1, ProfileID: "host"},
				writes:      make(chan outbound, 8),
			},
		},
		participants: map[string]int{"host": 1},
		transfer: &stateTransfer{
			id: "old-transfer", nextFrame: 0, reason: "INITIAL_SYNC",
		},
	}
	reconnected := &peer{
		participant: SocketParticipant{
			PlayerNo: 2, ProfileID: "guest", ProfileDigest: "profile", CoreArtifactID: "core",
		},
		writes: make(chan outbound, 8),
	}

	if err := session.addPeer(ctx, reconnected, -1); err != nil {
		t.Fatalf("reconnect during initial transfer: %v", err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return session.transfer == nil }, func() bool { return session.transfer.id == "old-transfer" }, func() bool { return session.transfer.reason != "INITIAL_SYNC" }), "initial transfer was not restarted: %#v", session.transfer)
	select {
	case message := <-session.peers[1].writes:
		var decoded map[string]any
		if err := json.Unmarshal(message.data, &decoded); err != nil {
			t.Fatal(err)
		}
		testassert.Falsef(t, testassert.Any(func() bool { return decoded["type"] != "REQUEST_STATE" }, func() bool { return decoded["transferId"] != session.transfer.id }), "restart request = %#v", decoded)
	default:
		t.Fatal("authority did not receive a restarted state request")
	}
}

func TestTransportLossDuringStateTransferStartsLeaseAndInvalidatesOldTransfer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := &Service{options: Options{ReconnectLease: time.Hour}}
	authority := &peer{
		participant: SocketParticipant{
			PlayerNo: 1, ProfileID: "host", ProfileDigest: "profile", CoreArtifactID: "core",
		},
		writes: make(chan outbound, 8),
	}
	disconnected := &peer{
		participant: SocketParticipant{
			PlayerNo: 2, ProfileID: "guest", ProfileDigest: "profile", CoreArtifactID: "core",
		},
		writes: make(chan outbound, 8),
	}
	session := &realtimeSession{
		service: service, sessionID: "session", profileDigest: "profile", coreArtifact: "core",
		occupiedMask: 3, playerCount: 2, peers: map[int]*peer{1: authority, 2: disconnected},
		participants: map[string]int{"host": 1, "guest": 2}, leaseTimers: make(map[int]*time.Timer),
		transfer: &stateTransfer{
			id: "old-transfer", nextFrame: 12, reason: "INITIAL_SYNC",
			targets: map[int]bool{2: true}, applied: make(map[int]bool),
		},
	}

	session.removePeer(ctx, disconnected)
	testassert.Falsef(t, testassert.Any(func() bool { return session.peers[2] != nil }, func() bool { return session.leaseTimers[2] == nil }), "transfer disconnect peer=%#v lease=%#v", session.peers[2], session.leaseTimers[2])
	invalidatedID := session.transfer.id
	testassert.False(t, invalidatedID == "old-transfer", "transport loss left the old transfer ID valid")
	if err := session.acceptStateMeta(ctx, authority, ClientMessage{
		TransferID: "old-transfer", NextFrame: 12, ByteLength: 1,
		StateSHA256: strings.Repeat("a", 64), CoreSHA256: strings.Repeat("b", 64),
	}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("old transfer metadata error = %v", err)
	}

	reconnected := &peer{participant: disconnected.participant, writes: make(chan outbound, 8)}
	if err := session.addPeer(ctx, reconnected, -1); err != nil {
		t.Fatalf("reconnect during transfer: %v", err)
	}
	testassert.False(t, session.leaseTimers[2] != nil, "successful reconnect did not stop the recovery lease")
	testassert.Falsef(t, testassert.Any(func() bool { return session.transfer == nil }, func() bool { return session.transfer.id == invalidatedID }, func() bool { return session.transfer.reason != "INITIAL_SYNC" }), "replacement transfer = %#v", session.transfer)
	select {
	case message := <-authority.writes:
		var decoded map[string]any
		if err := json.Unmarshal(message.data, &decoded); err != nil {
			t.Fatal(err)
		}
		testassert.Falsef(t, testassert.Any(func() bool { return decoded["type"] != "REQUEST_STATE" }, func() bool { return decoded["transferId"] != session.transfer.id }), "replacement request = %#v", decoded)
	default:
		t.Fatal("authority did not receive a replacement state request")
	}
}

func TestClientRuntimeEndReasonIsAClosedSet(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{
		"USER_EXIT", "ROLLBACK_WINDOW_EXCEEDED", "STATE_RING_CAPACITY_EXCEEDED", "STATE_INVALID",
		"NETPLAY_UNSTABLE", "INTERNAL_ERROR", "PROTOCOL_VIOLATION",
	} {
		testassert.Truef(t, allowedClientEndReason(reason), "allowed client reason %q was rejected", reason)
	}
	for _, reason := range []string{"", "AUTH_REVOKED", "PEER_TIMEOUT", "arbitrary error"} {
		testassert.Falsef(t, allowedClientEndReason(reason), "untrusted client reason %q was accepted", reason)
	}
}

func TestQueueOverflowDropsOnlyTheAffectedTransport(t *testing.T) {
	for _, test := range []struct {
		name       string
		fillQueue  bool
		queuedByte int
	}{
		{name: "message count", fillQueue: true},
		{name: "byte count", queuedByte: MaxWSMessageBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			serverConnection, remote := websocketPair(t)
			client := &peer{
				participant: SocketParticipant{PlayerNo: 2, ProfileID: "guest"},
				connection:  serverConnection,
				writes:      make(chan outbound, 256),
				queuedBytes: test.queuedByte,
			}
			if test.fillQueue {
				for range 256 {
					client.writes <- outbound{data: []byte{1}}
				}
			}
			session := &realtimeSession{}
			flushed := session.sendTrackedLocked(context.Background(), client, websocket.MessageText, []byte("overflow"))
			select {
			case <-flushed:
			default:
				t.Fatal("overflow did not release its terminal flush waiter")
			}
			readContext, cancelRead := context.WithTimeout(context.Background(), time.Second)
			defer cancelRead()
			if _, _, err := remote.Read(readContext); err == nil {
				t.Fatal("overflow did not close the affected transport")
			}
			testassert.False(t, session.ended, "one peer queue overflow ended the whole session")
		})
	}
}

func TestPingAndWriteFailuresDropOnlyTheirTransportAndReleaseFlushes(t *testing.T) {
	t.Run("ping", func(t *testing.T) {
		serverConnection, remote := websocketPair(t)
		_ = remote.CloseNow()
		client := &peer{connection: serverConnection}
		session := &realtimeSession{}
		testassert.False(t, client.validateAndPing(context.Background(), session), "ping unexpectedly succeeded after the remote transport closed")
		testassert.False(t, session.ended, "ping failure ended the whole session")
	})

	t.Run("write", func(t *testing.T) {
		serverConnection, _ := websocketPair(t)
		_ = serverConnection.CloseNow()
		flushed := make(chan struct{})
		client := &peer{connection: serverConnection, writes: make(chan outbound, 2), queuedBytes: 2}
		client.writes <- outbound{kind: websocket.MessageText, data: []byte("a"), flushed: flushed}
		client.writes <- outbound{kind: websocket.MessageText, data: []byte("b"), flushed: make(chan struct{})}
		close(client.writes)
		done := make(chan struct{})
		client.writeLoop(context.Background(), done)
		<-done
		select {
		case <-flushed:
		default:
			t.Fatal("failed write did not release the in-flight flush")
		}
		client.mu.Lock()
		writeStopped, queuedBytes := client.writeStopped, client.queuedBytes
		client.mu.Unlock()
		testassert.Falsef(t, testassert.Any(func() bool { return !writeStopped }, func() bool { return queuedBytes != 0 }), "write cleanup stopped=%v queuedBytes=%d", writeStopped, queuedBytes)
	})
}

func TestSessionValidationToleratesTwoUnavailableChecksAndResetsOnSuccess(t *testing.T) {
	t.Parallel()
	results := []SessionValidation{SessionUnavailable, SessionUnavailable, SessionValid, SessionUnavailable, SessionUnavailable, SessionUnavailable}
	client := &peer{validator: func(context.Context, string, string) SessionValidation {
		result := results[0]
		results = results[1:]
		return result
	}}
	for index, wantDrop := range []bool{false, false, false, false, false, true} {
		validation, drop := client.validateSession(context.Background())
		testassert.Falsef(t, testassert.All(func() bool { return index == 2 }, func() bool { return validation != SessionValid }), "successful validation = %v", validation)
		testassert.Falsef(t, drop != wantDrop, "validation %d drop=%v want=%v", index+1, drop, wantDrop)
	}
	client.validator = func(context.Context, string, string) SessionValidation { return SessionRevoked }
	validation, drop := client.validateSession(context.Background())
	testassert.Falsef(t, testassert.Any(func() bool { return validation != SessionRevoked }, func() bool { return drop }), "explicit revocation = %v/%v", validation, drop)
}

func TestPauseBarrierRequiresEveryOccupiedPlayerAndReconnectHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	session := &realtimeSession{
		sessionID: "session", occupiedMask: 3, playerCount: 2, nextFrame: 120, running: true,
		peers: map[int]*peer{
			1: {participant: SocketParticipant{PlayerNo: 1}, writes: make(chan outbound, 8)},
			2: {participant: SocketParticipant{PlayerNo: 2}, writes: make(chan outbound, 8)},
		},
	}
	session.mu.Lock()
	session.beginPauseLocked(ctx, "HOST_PAUSE", 1, pauseActionHost)
	session.mu.Unlock()
	if err := session.acceptPaused(ctx, session.peers[1]); err != nil {
		t.Fatal(err)
	}
	testassert.False(t, allAcknowledged(session.pause.paused), "pause completed before P2 acknowledged")
	if err := session.acceptPaused(ctx, session.peers[2]); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return !allAcknowledged(session.pause.paused) }, func() bool { return session.running }), "pause barrier = %#v running=%v", session.pause, session.running)

	session.pause = &pauseBarrier{
		action: pauseActionReconnect, paused: map[int]bool{1: false, 2: false},
		historyApplied: map[int]bool{2: false}, historyAppliedUntil: make(map[int]int64),
	}
	if err := session.acceptHistoryApplied(ctx, session.peers[2], 118); !errors.Is(err, ErrProtocol) {
		t.Fatalf("wrong history boundary error = %v", err)
	}
	if err := session.acceptHistoryApplied(ctx, session.peers[2], 119); err != nil {
		t.Fatal(err)
	}
	if err := session.acceptPaused(ctx, session.peers[2]); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return session.pause == nil }, func() bool { return !session.pause.historyApplied[2] }, func() bool { return !session.pause.paused[2] }), "reconnect acknowledgements = %#v", session.pause)
}

func TestAcceptanceNP012CanonicalFramesAreAtomicForTwoToFourPlayers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	playerValue := [...]int16{0, 1, 2, 3, 4}
	for players := 2; players <= 4; players++ {
		t.Run(strconv.Itoa(players)+" players", func(t *testing.T) {
			mask := (1 << players) - 1
			session := &realtimeSession{
				occupiedMask: mask, playerCount: players, running: true,
				peers: make(map[int]*peer), inputs: make(map[int64]map[int][24]int16),
			}
			for playerNo := players; playerNo >= 1; playerNo-- {
				controls := make([]int16, ControlCount)
				controls[0] = playerValue[playerNo]
				if err := session.acceptInput(ctx, playerNo, 0, controls); err != nil {
					t.Fatal(err)
				}
				testassert.False(t, testassert.All(func() bool { return playerNo > 1 }, func() bool { return session.nextFrame != 0 }), "canonical frame published before every occupied seat contributed")
			}
			testassert.Falsef(t, testassert.Any(func() bool { return session.nextFrame != 1 }, func() bool { return len(session.history) != 1 }), "canonical state: next=%d history=%d", session.nextFrame, len(session.history))
			frame := session.history[0]
			for playerNo := 1; playerNo <= 4; playerNo++ {
				want := int16(0)
				if playerNo <= players {
					want = playerValue[playerNo]
				}
				testassert.Falsef(t, frame.Players[playerNo-1][0] != want, "P%d input=%d want=%d", playerNo, frame.Players[playerNo-1][0], want)
			}
		})
	}
}

func TestAcceptanceNP010InputReplayFutureMutationAndRoomIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	first := &realtimeSession{
		occupiedMask: 3, playerCount: 2, running: true,
		peers: make(map[int]*peer), inputs: make(map[int64]map[int][24]int16),
	}
	second := &realtimeSession{
		occupiedMask: 3, playerCount: 2, running: true,
		peers: make(map[int]*peer), inputs: make(map[int64]map[int][24]int16),
	}
	controls := make([]int16, ControlCount)
	if err := first.acceptInput(ctx, 1, 121, controls); !errors.Is(err, ErrProtocol) {
		t.Fatalf("future input error = %v", err)
	}
	if err := first.acceptInput(ctx, 1, 0, controls); err != nil {
		t.Fatal(err)
	}
	if err := first.acceptInput(ctx, 1, 0, controls); err != nil {
		t.Fatalf("byte-identical replay was not idempotent: %v", err)
	}
	mutated := make([]int16, ControlCount)
	mutated[0] = 1
	if err := first.acceptInput(ctx, 1, 0, mutated); !errors.Is(err, ErrProtocol) {
		t.Fatalf("mutated replay error = %v", err)
	}
	for playerNo := 1; playerNo <= 2; playerNo++ {
		input := make([]int16, ControlCount)
		input[0] = int16(playerNo)
		if err := second.acceptInput(ctx, playerNo, 0, input); err != nil {
			t.Fatal(err)
		}
	}
	testassert.Falsef(t, testassert.Any(func() bool { return second.nextFrame != 1 }, func() bool { return len(second.history) != 1 }), "independent room was polluted: next=%d history=%d", second.nextFrame, len(second.history))
}
