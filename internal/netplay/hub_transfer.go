package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func (session *realtimeSession) handleMismatchLocked(ctx context.Context) {
	now := session.service.clock.Now()
	cutoff := now.Add(-time.Minute)
	kept := session.resyncTimes[:0]
	for _, occurredAt := range session.resyncTimes {
		if !occurredAt.Before(cutoff) {
			kept = append(kept, occurredAt)
		}
	}
	session.resyncTimes = kept
	if len(session.resyncTimes) >= 3 {
		go session.fail(ctx, "NETPLAY_UNSTABLE", "")
		return
	}
	session.resyncTimes = append(session.resyncTimes, now)
	session.beginPauseLocked(ctx, "STATE_MISMATCH", 0, pauseActionHashResync)
}

func (session *realtimeSession) prepareHashResync(ctx context.Context) {
	if err := session.service.PrepareHashResync(ctx, session.roomID, session.sessionID); err != nil {
		session.fail(ctx, "INTERNAL_ERROR", "")
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.ended && len(session.peers) == session.playerCount {
		session.beginTransferLocked(ctx, "STATE_MISMATCH", session.nextFrame)
	}
}

func (session *realtimeSession) beginTransferLocked(ctx context.Context, reason string, nextFrame int64) {
	if session.transfer != nil || session.ended {
		return
	}
	transferID := uuid.Must(uuid.NewV7()).String()
	targets := make(map[int]bool)
	for playerNo := 1; playerNo <= 4; playerNo++ {
		if session.occupiedMask&(1<<(playerNo-1)) != 0 && playerNo != 1 {
			targets[playerNo] = true
		}
	}
	transfer := &stateTransfer{
		id: transferID, nextFrame: nextFrame, reason: reason, targets: targets, applied: make(map[int]bool),
	}
	session.transfer = transfer
	if authority := session.peers[1]; authority != nil {
		session.sendLocked(ctx, authority, websocket.MessageText, session.serverMessageLocked("REQUEST_STATE", map[string]any{
			"transferId": transferID, "nextFrame": nextFrame, "targetPlayerNos": mapKeys(targets), "reason": reason,
		}))
	}
}

func (session *realtimeSession) acceptStateMeta(
	ctx context.Context,
	client *peer,
	message ClientMessage,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	transfer := session.transfer
	if client.participant.PlayerNo != 1 || transfer == nil || message.TransferID != transfer.id ||
		message.NextFrame != transfer.nextFrame || message.ByteLength < 1 || message.ByteLength > MaxStateBytes ||
		!validDigest(message.StateSHA256) || !validDigest(message.CoreSHA256) {
		return ErrProtocol
	}
	transfer.length = message.ByteLength
	transfer.stateDigest = message.StateSHA256
	transfer.coreDigest = message.CoreSHA256
	for target := range transfer.targets {
		if peer := session.peers[target]; peer != nil {
			session.sendLocked(ctx, peer, websocket.MessageText, session.serverMessageLocked("STATE_META", map[string]any{
				"transferId": transfer.id, "nextFrame": transfer.nextFrame, "byteLength": transfer.length,
				"stateSha256": transfer.stateDigest, "coreSha256": transfer.coreDigest,
			}))
		}
	}
	return nil
}

func (session *realtimeSession) handleBinary(ctx context.Context, client *peer, contents []byte) error {
	frame, err := ParseStateFrame(contents)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	transfer := session.transfer
	if !session.validStateFrameLocked(client, frame, transfer) {
		return ErrProtocol
	}
	fullDigest, coreDigest, err := StateDigests(frame.Payload)
	if err != nil || fullDigest != transfer.stateDigest || coreDigest != transfer.coreDigest {
		return ErrProtocol
	}
	transfer.binarySent = true
	for target := range transfer.targets {
		if targetPeer := session.peers[target]; targetPeer != nil {
			session.sendLocked(ctx, targetPeer, websocket.MessageBinary, contents)
		}
	}
	return nil
}

func (session *realtimeSession) validStateFrameLocked(
	client *peer,
	frame StateFrame,
	transfer *stateTransfer,
) bool {
	return client.participant.PlayerNo == 1 && transfer != nil &&
		frame.SessionID.String() == session.sessionID && frame.Transfer.String() == transfer.id &&
		frame.Epoch == session.epoch && frame.NextFrame <= ^uint64(0)>>1 &&
		int64(frame.NextFrame) == transfer.nextFrame && len(frame.Payload) == transfer.length &&
		!transfer.binarySent
}

func (session *realtimeSession) acceptStateReady(
	ctx context.Context,
	client *peer,
	message ClientMessage,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	transfer := session.transfer
	if client.participant.PlayerNo != 1 || transfer == nil || message.TransferID != transfer.id ||
		!transfer.binarySent || message.StateSHA256 != transfer.stateDigest ||
		message.CoreSHA256 != transfer.coreDigest || !message.RecaptureMatched {
		return ErrProtocol
	}
	transfer.authorityOK = true
	session.maybeStartLocked(ctx)
	return nil
}

func (session *realtimeSession) acceptStateApplied(
	ctx context.Context,
	client *peer,
	message ClientMessage,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	transfer := session.transfer
	playerNo := client.participant.PlayerNo
	if transfer == nil || !transfer.targets[playerNo] || message.TransferID != transfer.id || !transfer.binarySent ||
		message.StateSHA256 != transfer.stateDigest || message.CoreSHA256 != transfer.coreDigest ||
		!message.NativeLoadCompleted || !message.RecaptureMatched {
		return ErrProtocol
	}
	transfer.applied[playerNo] = true
	session.maybeStartLocked(ctx)
	return nil
}

func (session *realtimeSession) maybeStartLocked(ctx context.Context) {
	transfer := session.transfer
	if transfer == nil || !transfer.authorityOK || len(transfer.applied) != len(transfer.targets) {
		return
	}
	session.transfer = nil
	session.epoch++
	session.nextFrame = transfer.nextFrame
	session.inputs = make(map[int64]map[int][24]int16)
	session.hashes = make(map[int64]map[int]string)
	session.running = true
	if err := session.service.MarkSessionRunning(ctx, session.roomID, session.sessionID); err != nil &&
		!errors.Is(err, ErrRoomConflict) {
		go session.fail(context.WithoutCancel(ctx), "INTERNAL_ERROR", "")
		return
	}
	session.broadcastLocked(ctx, "START_EPOCH", map[string]any{
		"epoch": session.epoch, "nextFrame": session.nextFrame, "occupiedSeatMask": session.occupiedMask,
	})
}

func (session *realtimeSession) serverMessageLocked(messageType string, fields map[string]any) []byte {
	session.serverSeq++
	value := map[string]any{
		"v": 1, "type": messageType, "sessionId": session.sessionID,
		"epoch": session.epoch, "seq": session.serverSeq,
	}
	for name, field := range fields {
		value[name] = field
	}
	encoded, _ := json.Marshal(value)
	return encoded
}

func (session *realtimeSession) broadcastLocked(ctx context.Context, messageType string, fields map[string]any) {
	encoded := session.serverMessageLocked(messageType, fields)
	for _, client := range session.peers {
		session.sendLocked(ctx, client, websocket.MessageText, encoded)
	}
}

func (session *realtimeSession) sendLocked(
	ctx context.Context,
	client *peer,
	kind websocket.MessageType,
	data []byte,
) {
	session.enqueueLocked(ctx, client, kind, data, nil)
}

func (session *realtimeSession) sendTrackedLocked(
	ctx context.Context,
	client *peer,
	kind websocket.MessageType,
	data []byte,
) <-chan struct{} {
	flushed := make(chan struct{})
	session.enqueueLocked(ctx, client, kind, data, flushed)
	return flushed
}

func (session *realtimeSession) enqueueLocked(
	_ context.Context,
	client *peer,
	kind websocket.MessageType,
	data []byte,
	flushed chan struct{},
) {
	client.mu.Lock()
	if client.writeStopped || len(client.writes) >= 256 || client.queuedBytes+len(data) > MaxWSMessageBytes {
		client.mu.Unlock()
		closeFlushSignal(flushed)
		client.dropTransport("")
		return
	}
	copyData := make([]byte, len(data))
	copy(copyData, data)
	client.queuedBytes += len(copyData)
	client.mu.Unlock()
	select {
	case client.writes <- outbound{kind: kind, data: copyData, flushed: flushed}:
	default:
		client.mu.Lock()
		client.queuedBytes -= len(copyData)
		client.mu.Unlock()
		closeFlushSignal(flushed)
		client.dropTransport("")
	}
}

func closeFlushSignal(flushed chan struct{}) {
	if flushed != nil {
		close(flushed)
	}
}

func (session *realtimeSession) fail(ctx context.Context, reason, profileID string) {
	workContext := context.WithoutCancel(ctx)
	session.mu.Lock()
	if session.ended {
		session.mu.Unlock()
		return
	}
	session.ended = true
	for _, timer := range session.leaseTimers {
		timer.Stop()
	}
	actorIsHost := session.participants[profileID] == 1
	if reason == "PEER_TIMEOUT" && actorIsHost {
		reason = "HOST_LOST"
	}
	disposition := endDisposition(reason, actorIsHost)
	terminal := session.serverMessageLocked("SESSION_ENDED", map[string]any{
		"reason": reason, "roomDisposition": disposition,
	})
	peers := make([]*peer, 0, len(session.peers))
	flushes := make([]<-chan struct{}, 0, len(session.peers))
	for _, client := range session.peers {
		peers = append(peers, client)
		flushes = append(flushes, session.sendTrackedLocked(workContext, client, websocket.MessageText, terminal))
	}
	session.mu.Unlock()
	actor := profileID
	if actor == "" && len(peers) > 0 {
		actor = peers[0].participant.ProfileID
	}
	var endErr error
	if actor == "" {
		endErr = session.service.endRoomSystem(workContext, session.roomID, reason)
	} else {
		endErr = session.service.EndRoom(workContext, session.roomID, actor, reason, nil)
	}
	if endErr != nil {
		slog.ErrorContext(workContext, "netplay terminal persistence failed",
			"roomId", session.roomID, "sessionId", session.sessionID, "reason", reason,
		)
	}
	session.hub.mu.Lock()
	if session.hub.sessions[session.roomID] == session {
		delete(session.hub.sessions, session.roomID)
	}
	session.hub.mu.Unlock()
	waitForTerminalFlushes(workContext, flushes)
	for _, client := range peers {
		_ = client.connection.Close(websocket.StatusNormalClosure, reason)
	}
}

func waitForTerminalFlushes(ctx context.Context, flushes []<-chan struct{}) {
	flushContext, cancelFlush := context.WithTimeout(ctx, 5*time.Second)
	defer cancelFlush()
	for _, flushed := range flushes {
		select {
		case <-flushed:
		case <-flushContext.Done():
		}
	}
}

func mapKeys(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func (hub *Hub) Close() {
	hub.mu.Lock()
	sessions := make([]*realtimeSession, 0, len(hub.sessions))
	for _, session := range hub.sessions {
		sessions = append(sessions, session)
	}
	hub.mu.Unlock()
	for _, session := range sessions {
		session.fail(context.Background(), "SERVER_RESTARTED", "")
	}
}

func (hub *Hub) Pause(ctx context.Context, roomID, sessionID, profileID string) error {
	hub.mu.Lock()
	session := hub.sessions[roomID]
	hub.mu.Unlock()
	if session == nil || session.sessionID != sessionID {
		return ErrSessionNotFound
	}
	session.mu.Lock()
	if session.ended || !session.running || session.peers[1] == nil ||
		session.peers[1].participant.ProfileID != profileID {
		session.mu.Unlock()
		return ErrForbidden
	}
	if err := session.service.SetSessionState(ctx, roomID, sessionID, profileID, "PAUSED_RECONNECT"); err != nil {
		session.mu.Unlock()
		return err
	}
	session.beginPauseLocked(ctx, "HOST_PAUSE", 1, pauseActionHost)
	session.mu.Unlock()
	return nil
}

func (hub *Hub) Resume(ctx context.Context, roomID, sessionID, profileID string) error {
	hub.mu.Lock()
	session := hub.sessions[roomID]
	hub.mu.Unlock()
	if session == nil || session.sessionID != sessionID {
		return ErrSessionNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.ended || session.running || len(session.peers) != session.playerCount || session.pause == nil ||
		session.pause.action != pauseActionHost || !allAcknowledged(session.pause.paused) ||
		session.peers[1] == nil || session.peers[1].participant.ProfileID != profileID {
		return ErrForbidden
	}
	if err := session.service.PrepareHostResync(ctx, roomID, sessionID); err != nil {
		return err
	}
	session.pause = nil
	session.beginTransferLocked(ctx, "HOST_RESUME", session.nextFrame)
	return nil
}

func (hub *Hub) Terminate(ctx context.Context, roomID, reason string) {
	hub.mu.Lock()
	session := hub.sessions[roomID]
	delete(hub.sessions, roomID)
	hub.mu.Unlock()
	if session == nil {
		return
	}
	session.mu.Lock()
	if session.ended {
		session.mu.Unlock()
		return
	}
	session.ended = true
	for _, timer := range session.leaseTimers {
		timer.Stop()
	}
	session.broadcastLocked(ctx, "SESSION_ENDED", map[string]any{
		"reason": reason, "roomDisposition": endDisposition(reason, false),
	})
	peers := make([]*peer, 0, len(session.peers))
	for _, client := range session.peers {
		peers = append(peers, client)
	}
	session.mu.Unlock()
	for _, client := range peers {
		_ = client.connection.Close(websocket.StatusNormalClosure, reason)
	}
}

func (hub *Hub) String() string {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return fmt.Sprintf("netplay hub (%d rooms)", len(hub.sessions))
}
