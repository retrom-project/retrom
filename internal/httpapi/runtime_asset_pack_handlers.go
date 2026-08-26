package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/rpgmaker/packs"
)

func (server *Server) runtimeAssetPacks(writer http.ResponseWriter, request *http.Request) {
	result, err := server.runtimePacks.List(request.Context())
	if err != nil {
		server.databaseError(writer, request, fmt.Errorf("list runtime asset packs: %w", err))
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) installRuntimeAssetPack(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(
			writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY",
			"幂等键无效", map[string]any{},
		)
		return
	}
	var body packs.InstallRequest
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(
			writer, request, http.StatusBadRequest, "INVALID_REQUEST",
			"运行包安装请求无效", map[string]any{},
		)
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	body.CreatorID = principal.UserID
	accepted, err := server.runtimePacks.Install(request.Context(), body)
	if err != nil {
		server.writeRuntimeAssetPackError(writer, request, err)
		return
	}
	writer.Header().Set("Location", "/api/v1/admin/jobs/"+accepted.JobID)
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusAccepted, accepted)
}

func (server *Server) deleteRuntimeAssetPack(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(
			writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY",
			"幂等键无效", map[string]any{},
		)
		return
	}
	if err := server.runtimePacks.Delete(
		request.Context(), request.PathValue("installationId"), version,
	); err != nil {
		server.writeRuntimeAssetPackError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) writeRuntimeAssetPackError(
	writer http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, packs.ErrInvalid):
		writeError(
			writer, request, http.StatusUnprocessableEntity, "RPG_RUNTIME_PACK_INVALID",
			"运行包内容或参数无效", map[string]any{},
		)
	case errors.Is(err, packs.ErrTooLarge):
		writeError(
			writer, request, http.StatusRequestEntityTooLarge, "RPG_RUNTIME_PACK_TOO_LARGE",
			"运行包超过文件数或容量限制", map[string]any{},
		)
	case errors.Is(err, packs.ErrUnavailable):
		writeError(
			writer, request, http.StatusServiceUnavailable, "RPG_RUNTIME_PACK_UNAVAILABLE",
			"运行包隔离校验资源暂时不可用，请稍后重试", map[string]any{},
		)
	case errors.Is(err, packs.ErrNotFound):
		writeError(
			writer, request, http.StatusNotFound, "RPG_RUNTIME_PACK_NOT_FOUND",
			"运行包安装不存在", map[string]any{},
		)
	case errors.Is(err, packs.ErrStale):
		writeError(
			writer, request, http.StatusPreconditionFailed, "RPG_RUNTIME_PACK_VERSION_CONFLICT",
			"运行包安装已发生变化", map[string]any{},
		)
	case errors.Is(err, packs.ErrReferenced):
		writeError(
			writer, request, http.StatusConflict, "RPG_RUNTIME_PACK_IN_USE",
			"运行包仍被游戏版本或存档引用", map[string]any{},
		)
	case errors.Is(err, packs.ErrConflict):
		writeError(
			writer, request, http.StatusConflict, "RPG_RUNTIME_PACK_CONFLICT",
			"运行包上传已使用、内容重复或当前状态不允许该操作", map[string]any{},
		)
	default:
		server.databaseError(writer, request, fmt.Errorf("runtime asset pack: %w", err))
	}
}
