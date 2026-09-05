package netplay

import (
	"context"
	"fmt"
	"time"
)

// This closed dispatcher explicitly enumerates every accepted wire message type.
func (session *realtimeSession) handleMessage(
	ctx context.Context,
	client *peer,
	message ClientMessage,
	messageBytes int,
) error {
	switch message.Type {
	case "RUNTIME_READY":
		return session.handleRuntimeReady(ctx, client, message)
	case "INPUT":
		return session.handleInput(ctx, client, message, messageBytes)
	case "HASH":
		return session.acceptHash(ctx, client.participant.PlayerNo, message.Frame, message.CoreDigest)
	case "STATE_META":
		return session.acceptStateMeta(ctx, client, message)
	case "STATE_READY":
		return session.acceptStateReady(ctx, client, message)
	case "STATE_APPLIED":
		return session.acceptStateApplied(ctx, client, message)
	case "PAUSED":
		return session.acceptPaused(ctx, client)
	case "HISTORY_APPLIED":
		return session.acceptHistoryApplied(ctx, client, message.HistoryAppliedThrough)
	case "END_REQUEST":
		return session.handleEndRequest(ctx, client, message.Reason)
	default:
		return ErrProtocol
	}
}

func (session *realtimeSession) handleRuntimeReady(
	ctx context.Context,
	client *peer,
	message ClientMessage,
) error {
	if message.ProviderID != session.providerID || message.TargetID != session.targetID ||
		message.BundleSHA256 != session.bundleSHA256 {
		return ErrProtocol
	}
	allReady, err := session.service.MarkRuntimeReady(ctx, client.participant)
	if err != nil {
		return err
	}
	if allReady {
		session.mu.Lock()
		session.beginTransferLocked(ctx, "INITIAL_SYNC", 0)
		session.mu.Unlock()
	}
	return nil
}

func (session *realtimeSession) handleInput(
	ctx context.Context,
	client *peer,
	message ClientMessage,
	messageBytes int,
) error {
	if messageBytes > MaxInputMessageBytes || message.PlayerNo != client.participant.PlayerNo ||
		len(message.Controls) != ControlCount || !client.allowInput(session.service.clock.Now()) {
		return ErrProtocol
	}
	return session.acceptInput(ctx, client.participant.PlayerNo, message.Frame, message.Controls)
}

func (session *realtimeSession) handleEndRequest(ctx context.Context, client *peer, reason string) error {
	if !allowedClientEndReason(reason) {
		return ErrProtocol
	}
	session.fail(ctx, reason, client.participant.ProfileID)
	return nil
}

func allowedClientEndReason(reason string) bool {
	switch reason {
	case "USER_EXIT", "ROLLBACK_WINDOW_EXCEEDED", "STATE_RING_CAPACITY_EXCEEDED", "STATE_INVALID",
		"NETPLAY_UNSTABLE", "INTERNAL_ERROR", "PROTOCOL_VIOLATION":
		return true
	default:
		return false
	}
}

func (client *peer) allowInput(now time.Time) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.inputRefill.IsZero() {
		client.inputRefill = now
		client.inputTokens = 240
	}
	elapsed := now.Sub(client.inputRefill).Seconds()
	if elapsed > 0 {
		client.inputTokens = min(240, client.inputTokens+elapsed*120)
		client.inputRefill = now
	}
	if client.inputTokens < 1 {
		return false
	}
	client.inputTokens--
	return true
}

func (session *realtimeSession) beginPauseLocked(
	ctx context.Context,
	reason string,
	affectedPlayerNo int,
	action string,
) {
	session.running = false
	paused := make(map[int]bool, session.playerCount)
	for playerNo := 1; playerNo <= 4; playerNo++ {
		if session.occupiedMask&(1<<(playerNo-1)) != 0 {
			paused[playerNo] = false
		}
	}
	session.pause = &pauseBarrier{
		reason: reason, atFrame: session.nextFrame - 1, action: action, paused: paused,
		historyApplied: make(map[int]bool), historyAppliedUntil: make(map[int]int64),
	}
	if action == pauseActionReconnect {
		session.pause.historyApplied[affectedPlayerNo] = false
	}
	session.broadcastLocked(ctx, "PAUSE", map[string]any{
		"reason": reason, "atFrame": session.pause.atFrame, "affectedPlayerNo": affectedPlayerNo,
	})
}

func (session *realtimeSession) acceptPaused(ctx context.Context, client *peer) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	barrier := session.pause
	playerNo := client.participant.PlayerNo
	if barrier == nil {
		return ErrProtocol
	}
	if _, expected := barrier.paused[playerNo]; !expected {
		return ErrProtocol
	}
	barrier.paused[playerNo] = true
	session.completePauseLocked(context.WithoutCancel(ctx))
	return nil
}

func (session *realtimeSession) acceptHistoryApplied(
	ctx context.Context,
	client *peer,
	appliedThrough int64,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	barrier := session.pause
	playerNo := client.participant.PlayerNo
	if barrier == nil || barrier.action != pauseActionReconnect ||
		appliedThrough != session.nextFrame-1 {
		return ErrProtocol
	}
	if _, expected := barrier.historyApplied[playerNo]; !expected {
		return ErrProtocol
	}
	barrier.historyApplied[playerNo] = true
	barrier.historyAppliedUntil[playerNo] = appliedThrough
	session.completePauseLocked(context.WithoutCancel(ctx))
	return nil
}

func allAcknowledged(values map[int]bool) bool {
	for _, acknowledged := range values {
		if !acknowledged {
			return false
		}
	}
	return true
}

func (session *realtimeSession) completePauseLocked(ctx context.Context) {
	barrier := session.pause
	if barrier == nil || !allAcknowledged(barrier.paused) || !allAcknowledged(barrier.historyApplied) {
		return
	}
	action := barrier.action
	switch action {
	case pauseActionHost:
		return
	case pauseActionReconnect:
		session.pause = nil
		go session.prepareResync(ctx)
	case pauseActionHashResync:
		session.pause = nil
		go session.prepareHashResync(ctx)
	default:
		session.pause = nil
	}
}

func (session *realtimeSession) acceptInput(
	ctx context.Context,
	playerNo int,
	frame int64,
	controls []int16,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if frame < 0 || frame > session.nextFrame+120 {
		return fmt.Errorf("input outside accepted window: %w", ErrProtocol)
	}
	// PAUSE is asynchronous with respect to frames already queued by a browser.
	// A valid current-epoch contribution that crosses that boundary is inert,
	// not a protocol attack; the next epoch clears the whole input map.
	if !session.running {
		return nil
	}
	if frame < session.nextFrame {
		return fmt.Errorf("input behind canonical frame: %w", ErrProtocol)
	}
	contributions := session.inputs[frame]
	if contributions == nil {
		contributions = make(map[int][24]int16)
		session.inputs[frame] = contributions
	}
	var value [24]int16
	copy(value[:], controls)
	if previous, exists := contributions[playerNo]; exists {
		if previous != value {
			return fmt.Errorf("input contribution mutated: %w", ErrProtocol)
		}
		return nil
	}
	contributions[playerNo] = value
	for {
		current := session.inputs[session.nextFrame]
		if len(current) != session.playerCount {
			break
		}
		frame := canonicalFrame{Frame: session.nextFrame, OccupiedSeatMask: session.occupiedMask}
		for player, contribution := range current {
			frame.Players[player-1] = contribution
		}
		session.broadcastLocked(ctx, "CANONICAL", map[string]any{
			"frame": frame.Frame, "occupiedSeatMask": frame.OccupiedSeatMask, "players": frame.Players,
		})
		session.history = append(session.history, frame)
		if len(session.history) > CanonicalHistoryFrames {
			session.history = session.history[len(session.history)-CanonicalHistoryFrames:]
		}
		delete(session.inputs, session.nextFrame)
		session.nextFrame++
	}
	return nil
}

func (session *realtimeSession) acceptHash(ctx context.Context, playerNo int, frame int64, digest string) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if !validDigest(digest) || frame < 0 || (frame+1)%CheckpointEveryFrames != 0 || frame >= session.nextFrame {
		return fmt.Errorf("hash outside canonical checkpoint: %w", ErrProtocol)
	}
	// The other peer's checkpoint can already be in flight when a mismatch
	// pauses the epoch. It cannot affect a paused session and must not turn a
	// deterministic resync into PROTOCOL_VIOLATION.
	if !session.running {
		return nil
	}
	values := session.hashes[frame]
	if values == nil {
		values = make(map[int]string)
		session.hashes[frame] = values
	}
	if previous, exists := values[playerNo]; exists && previous != digest {
		return fmt.Errorf("checkpoint hash mutated: %w", ErrProtocol)
	}
	values[playerNo] = digest
	if len(values) != session.playerCount {
		return nil
	}
	delete(session.hashes, frame)
	if matchingDigests(values) {
		return nil
	}
	session.handleMismatchLocked(context.WithoutCancel(ctx))
	return nil
}

func matchingDigests(values map[int]string) bool {
	first := ""
	for _, value := range values {
		if first == "" {
			first = value
		} else if value != first {
			return false
		}
	}
	return true
}
