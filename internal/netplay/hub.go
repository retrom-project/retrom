package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type SessionValidator func(context.Context, string, string) bool

type Hub struct {
	service  *Service
	mu       sync.Mutex
	sessions map[string]*realtimeSession
}

func NewHub(service *Service) *Hub {
	return &Hub{service: service, sessions: make(map[string]*realtimeSession)}
}

type outbound struct {
	kind    websocket.MessageType
	data    []byte
	flushed chan struct{}
}

type peer struct {
	mu          sync.Mutex
	participant SocketParticipant
	connection  *websocket.Conn
	writes      chan outbound
	queuedBytes int
	clientSeq   uint64
	inputTokens float64
	inputRefill time.Time
	authToken   string
	validator   SessionValidator
}

type canonicalFrame struct {
	Frame            int64        `json:"frame"`
	OccupiedSeatMask int          `json:"occupiedSeatMask"`
	Players          [4][24]int16 `json:"players"`
}

type stateTransfer struct {
	id          string
	nextFrame   int64
	reason      string
	targets     map[int]bool
	stateDigest string
	coreDigest  string
	length      int
	binarySent  bool
	authorityOK bool
	applied     map[int]bool
	timer       *time.Timer
}

type realtimeSession struct {
	mu            sync.Mutex
	hub           *Hub
	service       *Service
	roomID        string
	sessionID     string
	profileDigest string
	coreArtifact  string
	occupiedMask  int
	playerCount   int
	peers         map[int]*peer
	participants  map[string]int
	serverSeq     uint64
	epoch         uint32
	nextFrame     int64
	inputs        map[int64]map[int][24]int16
	history       []canonicalFrame
	hashes        map[int64]map[int]string
	resyncTimes   []time.Time
	transfer      *stateTransfer
	running       bool
	ended         bool
	leaseTimers   map[int]*time.Timer
	pause         *pauseBarrier
}

type pauseBarrier struct {
	reason              string
	atFrame             int64
	action              string
	paused              map[int]bool
	historyApplied      map[int]bool
	historyAppliedUntil map[int]int64
}

const (
	pauseActionReconnect  = "RECONNECT"
	pauseActionHashResync = "HASH_RESYNC"
	pauseActionHost       = "HOST_PAUSE"
)

func (hub *Hub) session(participant SocketParticipant) (*realtimeSession, error) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	current := hub.sessions[participant.RoomID]
	if current != nil {
		if current.sessionID != participant.SessionID {
			return nil, ErrRoomConflict
		}
		return current, nil
	}
	current = &realtimeSession{
		hub: hub, service: hub.service, roomID: participant.RoomID, sessionID: participant.SessionID,
		profileDigest: participant.ProfileDigest, coreArtifact: participant.CoreArtifactID,
		occupiedMask: participant.OccupiedSeatMask, playerCount: participant.PlayerCount,
		peers: make(map[int]*peer), participants: make(map[string]int), inputs: make(map[int64]map[int][24]int16),
		hashes: make(map[int64]map[int]string), leaseTimers: make(map[int]*time.Timer),
	}
	hub.sessions[participant.RoomID] = current
	return current, nil
}

func (hub *Hub) Connect(
	ctx context.Context,
	connection *websocket.Conn,
	participant SocketParticipant,
	authToken string,
	validator SessionValidator,
) error {
	session, err := hub.session(participant)
	if err != nil {
		return err
	}
	connection.SetReadLimit(MaxWSMessageBytes)
	client := &peer{
		participant: participant, connection: connection, writes: make(chan outbound, 256),
		authToken: authToken, validator: validator,
		inputTokens: 240, inputRefill: session.service.clock.Now(),
	}
	writerDone := make(chan struct{})
	go client.writeLoop(ctx, writerDone)
	defer func() {
		close(client.writes)
		<-writerDone
	}()
	hello, err := readHello(ctx, connection)
	if err != nil || !matchesHello(hello, session, participant) {
		return ErrProtocol
	}
	if err := session.addPeer(ctx, client, hello.LastCanonicalFrame); err != nil {
		return err
	}
	defer session.removePeer(ctx, client)

	validationContext, cancelValidation := context.WithCancel(ctx)
	validationDone := make(chan struct{})
	go client.validationLoop(validationContext, session, validationDone)
	defer func() {
		cancelValidation()
		<-validationDone
	}()
	return session.readMessages(ctx, client)
}

func readHello(ctx context.Context, connection *websocket.Conn) (ClientMessage, error) {
	helloContext, cancelHello := context.WithTimeout(ctx, 5*time.Second)
	kind, contents, err := connection.Read(helloContext)
	cancelHello()
	if err != nil || kind != websocket.MessageText {
		return ClientMessage{}, ErrProtocol
	}
	hello, err := DecodeClientMessage(contents)
	if err != nil {
		return ClientMessage{}, err
	}
	return hello, nil
}

func matchesHello(hello ClientMessage, session *realtimeSession, participant SocketParticipant) bool {
	return hello.Type == "HELLO" && hello.Seq == 0 && hello.Epoch == session.epoch &&
		hello.SessionID == participant.SessionID && hello.ProtocolVersion == ProtocolVersion &&
		hello.ProfileDigest == participant.ProfileDigest && hello.PlayerNo == participant.PlayerNo &&
		hello.CredentialGeneration == participant.CredentialGeneration && hello.LastCanonicalFrame < session.nextFrame
}

func (client *peer) validationLoop(
	ctx context.Context,
	session *realtimeSession,
	done chan<- struct{},
) {
	defer close(done)
	authTicker := time.NewTicker(5 * time.Second)
	defer authTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-authTicker.C:
			if !client.validateAndPing(ctx, session) {
				return
			}
		}
	}
}

func (client *peer) validateAndPing(ctx context.Context, session *realtimeSession) bool {
	if client.validator != nil && !client.validator(ctx, client.authToken, client.participant.ProfileID) {
		session.fail(ctx, "AUTH_REVOKED", client.participant.ProfileID)
		_ = client.connection.Close(websocket.StatusPolicyViolation, "authentication revoked")
		return false
	}
	pingContext, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	err := client.connection.Ping(pingContext)
	cancelPing()
	return err == nil
}

func (session *realtimeSession) readMessages(ctx context.Context, client *peer) error {
	for {
		kind, data, err := client.connection.Read(ctx)
		if err != nil {
			return fmt.Errorf("netplay/read socket: %w", err)
		}
		if err := session.handleFrame(ctx, client, kind, data); err != nil {
			reason := "INTERNAL_ERROR"
			if errors.Is(err, ErrProtocol) {
				reason = "PROTOCOL_VIOLATION"
			}
			slog.WarnContext(ctx, "netplay client message rejected",
				"roomId", session.roomID, "sessionId", session.sessionID,
				"playerNo", client.participant.PlayerNo, "reason", reason, "error", err,
			)
			session.fail(ctx, reason, client.participant.ProfileID)
			return err
		}
	}
}

func (session *realtimeSession) handleFrame(
	ctx context.Context,
	client *peer,
	kind websocket.MessageType,
	data []byte,
) error {
	if kind == websocket.MessageBinary {
		if err := session.handleBinary(ctx, client, data); err != nil {
			return fmt.Errorf("binary state frame: %w", err)
		}
		return nil
	}
	if kind != websocket.MessageText {
		return ErrProtocol
	}
	message, err := DecodeClientMessage(data)
	if err != nil {
		return fmt.Errorf("decode text message: %w", ErrProtocol)
	}
	if message.Type == "HELLO" || message.Seq != client.clientSeq+1 ||
		message.SessionID != session.sessionID || message.Epoch != session.epoch {
		return fmt.Errorf("validate client envelope: %w", ErrProtocol)
	}
	client.clientSeq = message.Seq
	if err := session.handleMessage(ctx, client, message, len(data)); err != nil {
		return fmt.Errorf("handle %s: %w", message.Type, err)
	}
	return nil
}

func (client *peer) writeLoop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	for message := range client.writes {
		writeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := client.connection.Write(writeContext, message.kind, message.data)
		cancel()
		client.mu.Lock()
		client.queuedBytes -= len(message.data)
		client.mu.Unlock()
		if message.flushed != nil {
			close(message.flushed)
		}
		if err != nil {
			return
		}
	}
}

func (session *realtimeSession) addPeer(ctx context.Context, client *peer, lastCanonical int64) error {
	session.mu.Lock()
	if !session.peerAllowedLocked(client) {
		session.mu.Unlock()
		return ErrProtocol
	}
	reconnecting := false
	if timer := session.leaseTimers[client.participant.PlayerNo]; timer != nil {
		timer.Stop()
		delete(session.leaseTimers, client.participant.PlayerNo)
		reconnecting = true
	}
	if previous := session.peers[client.participant.PlayerNo]; previous != nil && previous != client {
		_ = previous.connection.Close(websocket.StatusPolicyViolation, "connection replaced")
	}
	session.peers[client.participant.PlayerNo] = client
	session.participants[client.participant.ProfileID] = client.participant.PlayerNo
	if err := session.sendPeerHistoryLocked(ctx, client, lastCanonical, reconnecting); err != nil {
		session.mu.Unlock()
		return err
	}
	session.mu.Unlock()
	return nil
}

func (session *realtimeSession) peerAllowedLocked(client *peer) bool {
	participant := client.participant
	return !session.ended && participant.ProfileDigest == session.profileDigest &&
		participant.CoreArtifactID == session.coreArtifact &&
		session.occupiedMask&(1<<(participant.PlayerNo-1)) != 0
}

func (session *realtimeSession) sendPeerHistoryLocked(
	ctx context.Context,
	client *peer,
	lastCanonical int64,
	reconnecting bool,
) error {
	start := int64(-1)
	end := session.nextFrame - 1
	if len(session.history) > 0 {
		start = session.history[0].Frame
	}
	session.sendLocked(ctx, client, websocket.MessageText, session.serverMessageLocked("WELCOME", map[string]any{
		"roomVersion": client.participant.RoomVersion, "sessionVersion": client.participant.SessionVersion,
		"leaseMs": session.service.options.ReconnectLease.Milliseconds(), "historyStartFrame": start,
		"historyEndFrame": end, "occupiedSeatMask": session.occupiedMask, "playerNo": client.participant.PlayerNo,
	}))
	if reconnecting {
		if lastCanonical+1 < start {
			return ErrProtocol
		}
		frames := make([]canonicalFrame, 0, end-lastCanonical)
		for _, frame := range session.history {
			if frame.Frame > lastCanonical {
				frames = append(frames, frame)
			}
		}
		session.sendLocked(ctx, client, websocket.MessageText, session.serverMessageLocked("HISTORY", map[string]any{
			"fromFrame": lastCanonical + 1, "toFrame": end, "canonical": frames,
		}))
		if session.pause == nil || session.pause.action != pauseActionReconnect {
			return ErrProtocol
		}
		session.sendLocked(ctx, client, websocket.MessageText, session.serverMessageLocked("PAUSE", map[string]any{
			"reason": session.pause.reason, "atFrame": session.pause.atFrame,
			"affectedPlayerNo": client.participant.PlayerNo,
		}))
	}
	return nil
}

func (session *realtimeSession) prepareResync(ctx context.Context) {
	if err := session.service.PrepareResync(ctx, session.roomID, session.sessionID); err != nil {
		session.fail(ctx, "INTERNAL_ERROR", "")
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.ended && len(session.peers) == session.playerCount {
		session.beginTransferLocked(ctx, "PEER_RECONNECTED", session.nextFrame)
	}
}

func (session *realtimeSession) removePeer(ctx context.Context, client *peer) {
	workContext := context.WithoutCancel(ctx)
	session.mu.Lock()
	if session.peers[client.participant.PlayerNo] != client || session.ended {
		session.mu.Unlock()
		return
	}
	delete(session.peers, client.participant.PlayerNo)
	if session.running {
		session.beginPauseLocked(workContext, "PEER_DISCONNECTED", client.participant.PlayerNo, pauseActionReconnect)
		playerNo := client.participant.PlayerNo
		profileID := client.participant.ProfileID
		participant := client.participant
		go func() {
			if err := session.service.MarkDisconnected(workContext, participant); err != nil {
				session.fail(workContext, "INTERNAL_ERROR", profileID)
			}
		}()
		session.leaseTimers[playerNo] = time.AfterFunc(session.service.options.ReconnectLease, func() {
			session.mu.Lock()
			missing := session.peers[playerNo] == nil && !session.ended
			session.mu.Unlock()
			if missing {
				session.fail(workContext, "PEER_TIMEOUT", profileID)
			}
		})
	}
	session.mu.Unlock()
}

//nolint:gocyclo // This closed dispatcher explicitly enumerates every accepted wire message type.
func (session *realtimeSession) handleMessage(
	ctx context.Context,
	client *peer,
	message ClientMessage,
	messageBytes int,
) error {
	switch message.Type {
	case "RUNTIME_READY":
		if message.AdapterID != NetplayAdapterID || message.CoreArtifactID != session.coreArtifact {
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
	case "INPUT":
		if messageBytes > MaxInputMessageBytes || message.PlayerNo != client.participant.PlayerNo ||
			len(message.Controls) != ControlCount {
			return ErrProtocol
		}
		if !client.allowInput(session.service.clock.Now()) {
			return ErrProtocol
		}
		return session.acceptInput(ctx, client.participant.PlayerNo, message.Frame, message.Controls)
	case "HASH":
		return session.acceptHash(ctx, client.participant.PlayerNo, message.Frame, message.CoreDigest)
	case "SUSPEND_REQUEST":
		if message.Reason != "HIDDEN" && message.Reason != "BLUR" {
			return ErrProtocol
		}
		// Compatibility no-op for an already-loaded client from before focus loss
		// stopped being a network lifecycle event. A live page retains its socket;
		// lockstep naturally waits if Chrome throttles its frame production.
		return nil
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
		if message.Reason != "USER_EXIT" {
			return ErrProtocol
		}
		session.fail(ctx, "USER_EXIT", client.participant.ProfileID)
		return nil
	default:
		return ErrProtocol
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
	if err := session.service.PrepareResync(ctx, session.roomID, session.sessionID); err != nil {
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
	workContext := context.WithoutCancel(ctx)
	transfer.timer = time.AfterFunc(15*time.Second, func() {
		session.fail(workContext, "STATE_TRANSFER_TIMEOUT", "")
	})
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
	transfer.timer.Stop()
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
	ctx context.Context,
	client *peer,
	kind websocket.MessageType,
	data []byte,
	flushed chan struct{},
) {
	client.mu.Lock()
	if len(client.writes) >= 256 || client.queuedBytes+len(data) > MaxWSMessageBytes {
		client.mu.Unlock()
		closeFlushSignal(flushed)
		go session.fail(context.WithoutCancel(ctx), "PEER_TOO_SLOW", client.participant.ProfileID)
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
		go session.fail(context.WithoutCancel(ctx), "PEER_TOO_SLOW", client.participant.ProfileID)
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
	if session.transfer != nil && session.transfer.timer != nil {
		session.transfer.timer.Stop()
	}
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
	if actor == "" {
		_ = session.service.endRoomSystem(workContext, session.roomID, reason)
	} else {
		_ = session.service.EndRoom(workContext, session.roomID, actor, reason, nil)
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
	if err := session.service.PrepareResync(ctx, roomID, sessionID); err != nil {
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
	if session.transfer != nil && session.transfer.timer != nil {
		session.transfer.timer.Stop()
	}
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
