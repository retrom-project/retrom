package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/cursor"
	"retrom/internal/launch"
	"retrom/internal/netplay"

	"github.com/coder/websocket"
)

func (server *Server) registerNetplayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/netplay/games", server.netplayGames)
	mux.HandleFunc("GET /api/v1/netplay/rooms", server.netplayRooms)
	mux.HandleFunc("POST /api/v1/netplay/rooms", server.createNetplayRoom)
	mux.HandleFunc("GET /api/v1/netplay/rooms/{roomId}", server.netplayRoom)
	mux.HandleFunc("DELETE /api/v1/netplay/rooms/{roomId}", server.deleteNetplayRoom)
	mux.HandleFunc("PUT /api/v1/netplay/rooms/{roomId}/game", server.selectNetplayGame)
	mux.HandleFunc("DELETE /api/v1/netplay/rooms/{roomId}/game", server.clearNetplayGame)
	mux.HandleFunc("PUT /api/v1/netplay/rooms/{roomId}/members/me/seat", server.setNetplaySeat)
	mux.HandleFunc("PUT /api/v1/netplay/rooms/{roomId}/members/me/ready", server.setNetplayReady)
	mux.HandleFunc("DELETE /api/v1/netplay/rooms/{roomId}/members/me", server.leaveNetplayRoom)
	mux.HandleFunc("DELETE /api/v1/netplay/rooms/{roomId}/members/{memberId}", server.kickNetplayMember)
	mux.HandleFunc("POST /api/v1/netplay/rooms/{roomId}/start", server.startNetplaySession)
	mux.HandleFunc("POST /api/v1/netplay/rooms/{roomId}/sessions/{sessionId}/launch", server.createNetplayLaunch)
	mux.HandleFunc("POST /api/v1/netplay/rooms/{roomId}/sessions/{sessionId}/pause", server.pauseNetplaySession)
	mux.HandleFunc("POST /api/v1/netplay/rooms/{roomId}/sessions/{sessionId}/resume", server.resumeNetplaySession)
	mux.HandleFunc("POST /api/v1/netplay/rooms/{roomId}/sessions/{sessionId}/end", server.endNetplaySession)
	mux.HandleFunc("GET /api/v1/netplay/rooms/{roomId}/events", server.netplayEvents)
	mux.HandleFunc("GET /runtime/netplay/rooms/{roomId}/socket", server.netplaySocket)
}

func netplayProfileID(request *http.Request) string {
	principal, _ := authn.PrincipalFromContext(request.Context())
	return principal.ProfileID
}

func writeNetplayRoom(writer http.ResponseWriter, status int, room netplay.Room) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, room.Version))
	writeJSON(writer, status, room)
}

func (server *Server) writeNetplayError(writer http.ResponseWriter, request *http.Request, err error) {
	writer.Header().Set("Cache-Control", "private, no-store")
	switch {
	case errors.Is(err, netplay.ErrInvalidSeat):
		writeError(writer, request, http.StatusBadRequest, "NETPLAY_INVALID_SEAT", "联机座位无效", map[string]any{})
	case errors.Is(err, netplay.ErrInvalidProfile):
		writeError(writer, request, http.StatusBadRequest, "NETPLAY_INVALID_PROFILE", "联机配置无效", map[string]any{})
	case errors.Is(err, netplay.ErrForbidden):
		writeError(writer, request, http.StatusForbidden, "NETPLAY_FORBIDDEN", "无权执行该联机操作", map[string]any{})
	case errors.Is(err, netplay.ErrRoomNotFound):
		writeError(writer, request, http.StatusNotFound, "NETPLAY_ROOM_NOT_FOUND", "房间不存在或已失效", map[string]any{})
	case errors.Is(err, netplay.ErrSessionNotFound):
		writeError(writer, request, http.StatusNotFound, "NETPLAY_SESSION_NOT_FOUND", "联机会话不存在", map[string]any{})
	case errors.Is(err, netplay.ErrSeatTaken):
		writeError(writer, request, http.StatusConflict, "NETPLAY_SEAT_TAKEN", "座位已被占用", map[string]any{})
	case errors.Is(err, netplay.ErrRoomNotReady):
		writeError(writer, request, http.StatusConflict, "NETPLAY_ROOM_NOT_READY", "房间尚未准备完成", map[string]any{})
	case errors.Is(err, netplay.ErrRoomConflict):
		writeError(writer, request, http.StatusConflict, "NETPLAY_ROOM_STATE_CONFLICT", "房间状态已变化", map[string]any{})
	case errors.Is(err, netplay.ErrProfileStale):
		writeError(writer, request, http.StatusConflict, "NETPLAY_PROFILE_STALE", "联机配置已失效", map[string]any{})
	case errors.Is(err, netplay.ErrPrecondition):
		writeError(writer, request, http.StatusPreconditionFailed, "PRECONDITION_FAILED", "房间已被修改", map[string]any{})
	case errors.Is(err, netplay.ErrCapacity):
		writer.Header().Set("Retry-After", "5")
		writeError(writer, request, http.StatusTooManyRequests, "NETPLAY_CAPACITY_REACHED", "联机房间已达容量上限", map[string]any{})
	default:
		serverError(writer, request, err)
	}
}

func netplayLimit(request *http.Request, fallback int) int {
	if request.URL.Query().Get("limit") == "" {
		return fallback
	}
	value, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	return value
}

func (server *Server) netplayGames(writer http.ResponseWriter, request *http.Request) {
	availability := request.URL.Query().Get("availability")
	limit := netplayLimit(request, 100)
	filterDigest := cursor.FilterDigest(map[string]any{"availability": availability})
	afterTitle, afterGameID := "", ""
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, decodeErr := server.cursors.Decode(token, "getNetplayGames", filterDigest, "TITLE_ASC")
		if decodeErr != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterTitle, afterGameID = payload.SortValues[0], payload.ID
	}
	items, hasMore, err := server.netplay.GamePage(
		request.Context(), netplayProfileID(request), availability, afterTitle, afterGameID, limit,
	)
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	var next any
	if hasMore && len(items) > 0 {
		token, encodeErr := server.cursors.Encode(cursor.Payload{
			OperationID: "getNetplayGames", FilterDigest: filterDigest, SortCode: "TITLE_ASC",
			SortValues: []string{strings.ToLower(items[len(items)-1].Title)}, ID: items[len(items)-1].GameID,
		})
		if encodeErr != nil {
			serverError(writer, request, encodeErr)
			return
		}
		next = token
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) netplayRooms(writer http.ResponseWriter, request *http.Request) {
	view := request.URL.Query().Get("view")
	filterDigest := cursor.FilterDigest(map[string]any{"view": view})
	afterRoomID := ""
	afterUpdatedAtMS := int64(0)
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, decodeErr := server.cursors.Decode(token, "getNetplayRooms", filterDigest, "UPDATED_DESC")
		if decodeErr != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterUpdatedAtMS, decodeErr = strconv.ParseInt(payload.SortValues[0], 10, 64)
		if decodeErr != nil || afterUpdatedAtMS < 0 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterRoomID = payload.ID
	}
	items, hasMore, err := server.netplay.ListRooms(
		request.Context(), netplayProfileID(request), view,
		afterUpdatedAtMS, afterRoomID, netplayLimit(request, 24),
	)
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	var next any
	if hasMore && len(items) > 0 {
		token, encodeErr := server.cursors.Encode(cursor.Payload{
			OperationID: "getNetplayRooms", FilterDigest: filterDigest, SortCode: "UPDATED_DESC",
			SortValues: []string{strconv.FormatInt(items[len(items)-1].UpdatedAtMS, 10)}, ID: items[len(items)-1].RoomID,
		})
		if encodeErr != nil {
			serverError(writer, request, encodeErr)
			return
		}
		next = token
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) createNetplayRoom(writer http.ResponseWriter, request *http.Request) {
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "创建房间请求无效", map[string]any{})
		return
	}
	room, err := server.netplay.CreateRoom(request.Context(), netplayProfileID(request))
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	writer.Header().Set("Location", "/api/v1/netplay/rooms/"+room.RoomID)
	writeNetplayRoom(writer, http.StatusCreated, room)
}

func (server *Server) netplayRoom(writer http.ResponseWriter, request *http.Request) {
	room, err := server.netplay.Room(request.Context(), request.PathValue("roomId"), netplayProfileID(request))
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	writeNetplayRoom(writer, http.StatusOK, room)
}

func roomVersion(request *http.Request) (int64, error) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil || version < 1 {
		return 0, netplay.ErrPrecondition
	}
	return version, nil
}

func (server *Server) selectNetplayGame(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		GameID           string `json:"gameId"`
		NetplayProfileID string `json:"netplayProfileId"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "选择游戏请求无效", map[string]any{})
		return
	}
	server.mutateNetplayRoom(writer, request, func(ctx context.Context, version int64) (netplay.Room, error) {
		return server.netplay.SelectGame(
			ctx, request.PathValue("roomId"), netplayProfileID(request),
			body.GameID, body.NetplayProfileID, version,
		)
	})
}

func (server *Server) clearNetplayGame(writer http.ResponseWriter, request *http.Request) {
	server.mutateNetplayRoom(writer, request, func(ctx context.Context, version int64) (netplay.Room, error) {
		return server.netplay.ClearGame(
			ctx, request.PathValue("roomId"), netplayProfileID(request), version,
		)
	})
}

func (server *Server) setNetplaySeat(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		PlayerNo int `json:"playerNo"`
	}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "座位请求无效", map[string]any{})
		return
	}
	server.mutateNetplayRoom(writer, request, func(ctx context.Context, version int64) (netplay.Room, error) {
		return server.netplay.SetSeat(
			ctx, request.PathValue("roomId"), netplayProfileID(request), body.PlayerNo, version,
		)
	})
}

func (server *Server) setNetplayReady(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Ready bool `json:"ready"`
	}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "准备请求无效", map[string]any{})
		return
	}
	server.mutateNetplayRoom(writer, request, func(ctx context.Context, version int64) (netplay.Room, error) {
		return server.netplay.SetReady(
			ctx, request.PathValue("roomId"), netplayProfileID(request), body.Ready, version,
		)
	})
}

func (server *Server) mutateNetplayRoom(
	writer http.ResponseWriter,
	request *http.Request,
	mutation func(context.Context, int64) (netplay.Room, error),
) {
	version, err := roomVersion(request)
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	room, err := mutation(request.Context(), version)
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	writeNetplayRoom(writer, http.StatusOK, room)
}

func (server *Server) leaveNetplayRoom(writer http.ResponseWriter, request *http.Request) {
	version, err := roomVersion(request)
	if err == nil {
		err = server.netplay.Leave(request.Context(), request.PathValue("roomId"), netplayProfileID(request), version)
	}
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	server.netplayHub.Terminate(request.Context(), request.PathValue("roomId"), "USER_EXIT")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) kickNetplayMember(writer http.ResponseWriter, request *http.Request) {
	version, err := roomVersion(request)
	if err == nil {
		err = server.netplay.Kick(
			request.Context(), request.PathValue("roomId"), netplayProfileID(request),
			request.PathValue("memberId"), version,
		)
	}
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) startNetplaySession(writer http.ResponseWriter, request *http.Request) {
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "开始请求无效", map[string]any{})
		return
	}
	version, err := roomVersion(request)
	if err == nil {
		var room netplay.Room
		room, err = server.netplay.Start(request.Context(), request.PathValue("roomId"), netplayProfileID(request), version)
		if err == nil {
			writeNetplayRoom(writer, http.StatusAccepted, room)
			return
		}
	}
	server.writeNetplayError(writer, request, err)
}

func (server *Server) deleteNetplayRoom(writer http.ResponseWriter, request *http.Request) {
	version, err := roomVersion(request)
	if err == nil {
		err = server.netplay.EndRoom(
			request.Context(), request.PathValue("roomId"), netplayProfileID(request), "HOST_CLOSED", &version,
		)
	}
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	server.netplayHub.Terminate(request.Context(), request.PathValue("roomId"), "HOST_CLOSED")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) createNetplayLaunch(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		ClientCapabilities launch.Capabilities `json:"clientCapabilities"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "联机启动请求无效", map[string]any{})
		return
	}
	created, err := server.netplay.CreateParticipantLaunch(
		request.Context(), server.launcher, request.PathValue("roomId"), request.PathValue("sessionId"),
		netplayProfileID(request), body.ClientCapabilities,
	)
	if err != nil {
		if errors.Is(err, launch.ErrBlocked) {
			writeError(writer, request, http.StatusConflict, "NETPLAY_PROFILE_STALE", "联机启动配置已失效", map[string]any{})
			return
		}
		server.writeNetplayError(writer, request, err)
		return
	}
	server.setLaunchCookieValue(writer, created.Launch.LaunchID, created.Launch.Capability)
	server.setNetplayCookie(writer, request.PathValue("roomId"), created.RoomCapability)
	status := http.StatusCreated
	if created.Launch.Existing {
		status = http.StatusOK
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, status, map[string]any{
		"launchId": created.Launch.LaunchID, "playUrl": created.Launch.PlayURL,
		"warnings": created.Launch.Warnings, "bootstrapExpiresAtMs": created.Launch.BootstrapExpiresAtMS,
		"hardExpiresAtMs": created.Launch.HardExpiresAtMS,
	})
}

func (server *Server) setNetplayCookie(writer http.ResponseWriter, roomID, capability string) {
	if capability == "" {
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: "retrom_netplay_" + strings.ReplaceAll(roomID, "-", ""), Value: capability,
		Path: "/runtime/netplay/rooms/" + roomID + "/", MaxAge: 28800, HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: server.config.PublicOrigin.Scheme == "https",
	})
}

func (server *Server) reissueNetplayCookies(
	writer http.ResponseWriter, request *http.Request, responseBody []byte,
) error {
	segments := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(segments) != 8 || segments[0] != "api" || segments[1] != "v1" || segments[2] != "netplay" ||
		segments[3] != "rooms" || segments[5] != "sessions" || segments[7] != "launch" {
		return netplay.ErrForbidden
	}
	var response struct {
		LaunchID string `json:"launchId"`
	}
	if json.Unmarshal(responseBody, &response) != nil || response.LaunchID == "" {
		return netplay.ErrForbidden
	}
	capability, err := server.netplay.ParticipantCapability(
		request.Context(), segments[6], netplayProfileID(request),
	)
	if err != nil {
		return fmt.Errorf("reissue netplay capability: %w", err)
	}
	server.setLaunchCookie(writer, response.LaunchID)
	server.setNetplayCookie(writer, segments[4], capability)
	return nil
}

func decodeEmptyNetplayAction(writer http.ResponseWriter, request *http.Request) bool {
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "联机会话请求无效", map[string]any{})
		return false
	}
	return true
}

func (server *Server) pauseNetplaySession(writer http.ResponseWriter, request *http.Request) {
	if !decodeEmptyNetplayAction(writer, request) {
		return
	}
	if err := server.netplayHub.Pause(
		request.Context(), request.PathValue("roomId"), request.PathValue("sessionId"), netplayProfileID(request),
	); err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func (server *Server) resumeNetplaySession(writer http.ResponseWriter, request *http.Request) {
	if !decodeEmptyNetplayAction(writer, request) {
		return
	}
	if err := server.netplayHub.Resume(
		request.Context(), request.PathValue("roomId"), request.PathValue("sessionId"), netplayProfileID(request),
	); err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func (server *Server) endNetplaySession(writer http.ResponseWriter, request *http.Request) {
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "结束请求无效", map[string]any{})
		return
	}
	if err := server.netplay.EndRoom(
		request.Context(), request.PathValue("roomId"), netplayProfileID(request), "USER_EXIT", nil,
	); err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	server.netplayHub.Terminate(request.Context(), request.PathValue("roomId"), "USER_EXIT")
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func netplaySSEName(eventType string) string {
	if eventType == "ROOM_ENDED" || eventType == "ROOM_EXPIRED" {
		return "room.ended"
	}
	if strings.HasPrefix(eventType, "MEMBER_") || eventType == "SEAT_CHANGED" || eventType == "READY_CHANGED" {
		return "member.updated"
	}
	if strings.HasPrefix(eventType, "SESSION_") ||
		eventType == "PAUSED" || eventType == "RESUMED" || eventType == "RESYNCED" {
		return "session.updated"
	}
	return "room.updated"
}

func (server *Server) netplayEvents(writer http.ResponseWriter, request *http.Request) {
	roomID, profileID := request.PathValue("roomId"), netplayProfileID(request)
	if _, err := server.netplay.Room(request.Context(), roomID, profileID); err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	release, available := server.acquireNetplayObserver(roomID, profileID)
	if !available {
		writer.Header().Set("Retry-After", "5")
		writeError(
			writer, request, http.StatusTooManyRequests,
			"NETPLAY_RATE_LIMITED", "房间事件连接过多", map[string]any{},
		)
		return
	}
	defer release()
	lastID, err := parseNetplayEventID(request)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "事件游标无效", map[string]any{})
		return
	}
	server.streamNetplayEvents(writer, request, roomID, profileID, lastID)
}

func (server *Server) acquireNetplayObserver(roomID, profileID string) (func(), bool) {
	roomKey, profileKey := "room:"+roomID, "profile:"+roomID+":"+profileID
	server.netplayObserversMu.Lock()
	if server.netplayObservers[roomKey] >= 16 || server.netplayObservers[profileKey] >= 2 {
		server.netplayObserversMu.Unlock()
		return func() {}, false
	}
	server.netplayObservers[roomKey]++
	server.netplayObservers[profileKey]++
	server.netplayObserversMu.Unlock()
	return func() {
		server.netplayObserversMu.Lock()
		server.netplayObservers[roomKey]--
		server.netplayObservers[profileKey]--
		server.netplayObserversMu.Unlock()
	}, true
}

func parseNetplayEventID(request *http.Request) (int64, error) {
	if value := request.Header.Get("Last-Event-ID"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return 0, netplay.ErrRoomConflict
		}
		return parsed, nil
	}
	return 0, nil
}

func (server *Server) streamNetplayEvents(
	writer http.ResponseWriter,
	request *http.Request,
	roomID, profileID string,
	lastID int64,
) {
	setNetplayEventStreamHeaders(writer.Header())
	room, _ := server.netplay.Room(request.Context(), roomID, profileID)
	encoded, _ := json.Marshal(room)
	if err := server.writeSSE(writer, fmt.Sprintf("event: room.snapshot\ndata: %s\n\n", encoded)); err != nil {
		return
	}
	poll := time.NewTicker(500 * time.Millisecond)
	heartbeat := time.NewTicker(server.sseHeartbeat)
	closeAfter := time.NewTimer(30 * time.Minute)
	defer poll.Stop()
	defer heartbeat.Stop()
	defer closeAfter.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-closeAfter.C:
			return
		case <-heartbeat.C:
			if err := server.writeSSE(writer, ": heartbeat\n\n"); err != nil {
				return
			}
		case <-poll.C:
			events, err := server.netplay.Events(request.Context(), roomID, lastID, 100)
			if err != nil {
				return
			}
			var payload strings.Builder
			for _, event := range events {
				lastID = event.ID
				snapshot, snapshotErr := server.netplay.Room(request.Context(), roomID, profileID)
				if snapshotErr != nil {
					return
				}
				data, _ := json.Marshal(snapshot)
				_, _ = fmt.Fprintf(&payload, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, netplaySSEName(event.EventType), data)
			}
			if payload.Len() > 0 {
				if err := server.writeSSE(writer, payload.String()); err != nil {
					return
				}
			}
		}
	}
}

func setNetplayEventStreamHeaders(headers http.Header) {
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "private, no-store, no-transform")
	headers.Set("Content-Encoding", "identity")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")
}

func (server *Server) netplaySocket(writer http.ResponseWriter, request *http.Request) {
	if !server.validNetplaySocketRequest(request) {
		writeError(writer, request, http.StatusForbidden, "NETPLAY_ORIGIN_REJECTED", "联机连接来源无效", map[string]any{})
		return
	}
	roomID := request.PathValue("roomId")
	cookie, err := request.Cookie("retrom_netplay_" + strings.ReplaceAll(roomID, "-", ""))
	if err != nil {
		writeError(writer, request, http.StatusForbidden, "NETPLAY_FORBIDDEN", "联机凭据无效", map[string]any{})
		return
	}
	participant, err := server.netplay.AuthenticateSocket(
		request.Context(), roomID, netplayProfileID(request), cookie.Value,
	)
	if err != nil {
		server.writeNetplayError(writer, request, err)
		return
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols: []string{netplay.WebSocketSubprotocol}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer func() { _ = connection.CloseNow() }()
	validator := func(ctx context.Context, token, profileID string) netplay.SessionValidation {
		session, err := server.authenticator.Authenticate(ctx, token)
		if err == nil && session.Principal.ProfileID == profileID {
			return netplay.SessionValid
		}
		if err == nil || errors.Is(err, accounts.ErrAuthenticationNeeded) {
			return netplay.SessionRevoked
		}
		return netplay.SessionUnavailable
	}
	err = server.netplayHub.Connect(request.Context(), connection, participant, server.authCookieToken(request), validator)
	if err != nil {
		status := websocket.StatusPolicyViolation
		if !errors.Is(err, netplay.ErrProtocol) && !errors.Is(err, netplay.ErrForbidden) {
			status = websocket.StatusInternalError
		}
		_ = connection.Close(status, "netplay connection ended")
		return
	}
	_ = connection.Close(websocket.StatusNormalClosure, "netplay connection ended")
}

func (server *Server) validNetplaySocketRequest(request *http.Request) bool {
	origins := request.Header.Values("Origin")
	fetchSites := request.Header.Values("Sec-Fetch-Site")
	protocols := request.Header.Values("Sec-WebSocket-Protocol")
	return len(origins) == 1 && origins[0] == server.config.PublicOrigin.String() &&
		(len(fetchSites) == 0 || len(fetchSites) == 1 && fetchSites[0] == "same-origin") &&
		len(protocols) == 1 && protocols[0] == netplay.WebSocketSubprotocol
}
