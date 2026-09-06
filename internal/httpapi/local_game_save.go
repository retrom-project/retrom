package httpapi

import (
	"errors"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/saves"
)

func (server *Server) createLocalGameSave(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	if request.ContentLength > saves.MaxRequestBytes {
		writeError(writer, request, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "存档内容超过限制", map[string]any{})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, saves.MaxRequestBytes)
	principal, _ := authn.PrincipalFromContext(request.Context())
	result, replayed, err := server.saveService.CreateLocalDraft(request.Context(), request.PathValue("launchId"),
		principal.UserID, principal.ProfileID, key, request)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusForbidden, "FORBIDDEN", "本地草稿对应的游戏会话不可用", map[string]any{})
		return
	}
	writeSaveStateResult(writer, request, result, replayed, err)
}
