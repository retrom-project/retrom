package netplay

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

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
	if len(session.inputs) != 0 || len(session.hashes) != 0 {
		t.Fatalf("paused in-flight messages mutated session: inputs=%d hashes=%d", len(session.inputs), len(session.hashes))
	}
	if err := session.acceptInput(context.Background(), 2, 241, controls); !errors.Is(err, ErrProtocol) {
		t.Fatalf("out-of-window paused input error = %v, want protocol violation", err)
	}
}

func TestAcceptanceNP010InputRateLimitAllowsBurstAndRefillsAt120PerSecond(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_000_000, 0)
	client := &peer{inputTokens: 240, inputRefill: now}
	for index := 0; index < 240; index++ {
		if !client.allowInput(now) {
			t.Fatalf("burst contribution %d rejected", index+1)
		}
	}
	if client.allowInput(now) {
		t.Fatal("241st contribution in one burst was accepted")
	}
	now = now.Add(time.Second/120 + time.Nanosecond)
	if !client.allowInput(now) || client.allowInput(now) {
		t.Fatal("rate limiter did not refill exactly one token at 120/s")
	}
}

func TestLegacyFocusSuspendDoesNotDisconnectOrPauseTheSession(t *testing.T) {
	t.Parallel()
	client := &peer{participant: SocketParticipant{PlayerNo: 2}}
	session := &realtimeSession{running: true, nextFrame: 42}

	if err := session.handleMessage(context.Background(), client, ClientMessage{
		Type: "SUSPEND_REQUEST", Reason: "BLUR",
	}, 0); err != nil {
		t.Fatalf("legacy focus suspend = %v", err)
	}
	if !session.running || session.pause != nil || session.nextFrame != 42 {
		t.Fatalf("focus suspend changed live session: running=%v pause=%#v next=%d",
			session.running, session.pause, session.nextFrame)
	}
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
	if allAcknowledged(session.pause.paused) {
		t.Fatal("pause completed before P2 acknowledged")
	}
	if err := session.acceptPaused(ctx, session.peers[2]); err != nil {
		t.Fatal(err)
	}
	if !allAcknowledged(session.pause.paused) || session.running {
		t.Fatalf("pause barrier = %#v running=%v", session.pause, session.running)
	}

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
	if session.pause == nil || !session.pause.historyApplied[2] || !session.pause.paused[2] {
		t.Fatalf("reconnect acknowledgements = %#v", session.pause)
	}
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
				if playerNo > 1 && session.nextFrame != 0 {
					t.Fatal("canonical frame published before every occupied seat contributed")
				}
			}
			if session.nextFrame != 1 || len(session.history) != 1 {
				t.Fatalf("canonical state: next=%d history=%d", session.nextFrame, len(session.history))
			}
			frame := session.history[0]
			for playerNo := 1; playerNo <= 4; playerNo++ {
				want := int16(0)
				if playerNo <= players {
					want = playerValue[playerNo]
				}
				if frame.Players[playerNo-1][0] != want {
					t.Fatalf("P%d input=%d want=%d", playerNo, frame.Players[playerNo-1][0], want)
				}
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
	if second.nextFrame != 1 || len(second.history) != 1 {
		t.Fatalf("independent room was polluted: next=%d history=%d", second.nextFrame, len(second.history))
	}
}
