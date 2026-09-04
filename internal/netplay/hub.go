package netplay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type SessionValidation uint8

const (
	SessionValid SessionValidation = iota
	SessionRevoked
	SessionUnavailable
)

type SessionValidator func(context.Context, string, string) SessionValidation

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
	mu              sync.Mutex
	participant     SocketParticipant
	connection      *websocket.Conn
	writes          chan outbound
	queuedBytes     int
	clientSeq       uint64
	inputTokens     float64
	inputRefill     time.Time
	authToken       string
	validator       SessionValidator
	dropOnce        sync.Once
	writeStopped    bool
	authUnavailable int
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
}

type realtimeSession struct {
	mu            sync.Mutex
	hub           *Hub
	service       *Service
	roomID        string
	sessionID     string
	profileDigest string
	providerID    string
	targetID      string
	bundleSHA256  string
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
		profileDigest: participant.ProfileDigest, providerID: participant.ProviderID,
		targetID: participant.TargetID, bundleSHA256: participant.BundleSHA256,
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
	err = session.readMessages(ctx, client)
	if err != nil {
		status := websocket.CloseStatus(err)
		kind := "TRANSPORT_ERROR"
		switch {
		case status != -1:
			kind = "CLOSE_FRAME"
		case errors.Is(err, context.Canceled):
			kind = "REQUEST_CANCELED"
		case errors.Is(err, context.DeadlineExceeded):
			kind = "TIMEOUT"
		}
		slog.InfoContext(ctx, "netplay peer socket disconnected",
			"roomId", session.roomID, "sessionId", session.sessionID,
			"playerNo", participant.PlayerNo, "kind", kind, "closeStatus", int(status),
		)
	}
	return err
}

func readHello(ctx context.Context, connection *websocket.Conn) (ClientMessage, error) {
	helloContext, cancelHello := context.WithTimeout(ctx, 10*time.Second)
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
	if client.validator != nil {
		validation, shouldDrop := client.validateSession(ctx)
		switch validation {
		case SessionValid:
		case SessionRevoked:
			session.fail(ctx, "AUTH_REVOKED", client.participant.ProfileID)
			return false
		case SessionUnavailable:
			slog.WarnContext(ctx, "netplay peer authentication unavailable",
				"roomId", session.roomID, "sessionId", session.sessionID,
				"playerNo", client.participant.PlayerNo, "consecutive", client.authUnavailable,
			)
			if shouldDrop {
				client.dropTransport("")
				return false
			}
		}
	}
	pingContext, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	err := client.connection.Ping(pingContext)
	cancelPing()
	if err != nil {
		client.dropTransport("")
		return false
	}
	return true
}

func (client *peer) validateSession(ctx context.Context) (SessionValidation, bool) {
	validation := client.validator(ctx, client.authToken, client.participant.ProfileID)
	if validation == SessionUnavailable {
		client.authUnavailable++
		return validation, client.authUnavailable >= 3
	}
	client.authUnavailable = 0
	return validation, false
}

func (client *peer) dropTransport(reason string) {
	client.dropOnce.Do(func() {
		if client.connection != nil {
			if reason == "connection replaced" {
				_ = client.connection.Close(websocket.StatusPolicyViolation, reason)
			} else {
				_ = client.connection.CloseNow()
			}
		}
	})
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
			client.mu.Lock()
			client.writeStopped = true
			client.mu.Unlock()
			client.releaseQueuedFlushes()
			client.dropTransport("")
			return
		}
	}
}

func (client *peer) releaseQueuedFlushes() {
	for {
		select {
		case message, ok := <-client.writes:
			if !ok {
				return
			}
			client.mu.Lock()
			client.queuedBytes -= len(message.data)
			client.mu.Unlock()
			closeFlushSignal(message.flushed)
		default:
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
		previous.dropTransport("connection replaced")
	}
	session.peers[client.participant.PlayerNo] = client
	session.participants[client.participant.ProfileID] = client.participant.PlayerNo
	if err := session.sendPeerHistoryLocked(ctx, client, lastCanonical, reconnecting); err != nil {
		session.mu.Unlock()
		return err
	}
	session.restartTransferLocked(ctx)
	session.mu.Unlock()
	return nil
}

func (session *realtimeSession) restartTransferLocked(ctx context.Context) {
	transfer := session.transfer
	if session.running || transfer == nil || len(session.peers) != session.playerCount {
		return
	}
	session.transfer = nil
	session.beginTransferLocked(ctx, transfer.reason, transfer.nextFrame)
}

func (session *realtimeSession) peerAllowedLocked(client *peer) bool {
	participant := client.participant
	return !session.ended && participant.ProfileDigest == session.profileDigest &&
		participant.ProviderID == session.providerID && participant.TargetID == session.targetID &&
		participant.BundleSHA256 == session.bundleSHA256 &&
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
	if !reconnecting {
		return nil
	}
	if !session.running && (session.pause == nil || session.pause.action != pauseActionReconnect) {
		return nil
	}
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
	return nil
}

func (session *realtimeSession) prepareResync(ctx context.Context) {
	if err := session.service.PrepareReconnectResync(ctx, session.roomID, session.sessionID); err != nil {
		slog.ErrorContext(ctx, "netplay reconnect resync preparation failed",
			"roomId", session.roomID, "sessionId", session.sessionID, "error", err,
		)
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
	playerNo := client.participant.PlayerNo
	profileID := client.participant.ProfileID
	if session.running {
		session.beginPauseLocked(workContext, "PEER_DISCONNECTED", playerNo, pauseActionReconnect)
		participant := client.participant
		go func() {
			if err := session.service.MarkDisconnected(workContext, participant); err != nil {
				slog.ErrorContext(workContext, "netplay disconnect persistence failed",
					"roomId", session.roomID, "sessionId", session.sessionID, "playerNo", playerNo,
				)
			}
		}()
	} else {
		session.invalidateTransferLocked()
	}
	session.leaseTimers[playerNo] = time.AfterFunc(session.service.options.ReconnectLease, func() {
		session.mu.Lock()
		missing := session.peers[playerNo] == nil && !session.ended
		session.mu.Unlock()
		if missing {
			session.fail(workContext, "PEER_TIMEOUT", profileID)
		}
	})
	session.mu.Unlock()
}

func (session *realtimeSession) invalidateTransferLocked() {
	previous := session.transfer
	if previous == nil {
		return
	}
	targets := make(map[int]bool, len(previous.targets))
	for playerNo := range previous.targets {
		targets[playerNo] = true
	}
	session.transfer = &stateTransfer{
		id: uuid.Must(uuid.NewV7()).String(), nextFrame: previous.nextFrame,
		reason: previous.reason, targets: targets, applied: make(map[int]bool),
	}
}
