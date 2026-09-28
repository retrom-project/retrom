package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	launchservice "retrom/internal/service/launch"
	"retrom/internal/service/saves"
)

func (server *Server) createLaunch(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body launch.CreateRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "启动请求无效", map[string]any{})
		return
	}
	canonical, _ := json.Marshal(body)
	digestBytes := sha256.Sum256(append([]byte("postLaunch\x00"+principal.UserID+"\x00"), canonical...))
	receipt, err := server.playDeps.Launcher.CreateProduct(request.Context(), launchservice.ProductCreateCommand{
		ActorID: principal.UserID, ProfileID: principal.ProfileID, Key: key,
		Digest: hex.EncodeToString(digestBytes[:]), Request: body,
	})
	if err != nil {
		server.productCreationError(writer, request, err)
		return
	}
	server.writeStoredLaunchResponse(writer, receipt)
}

func (server *Server) writeStoredLaunchResponse(writer http.ResponseWriter, receipt launchservice.ProductReceipt) {
	if receipt.Status == http.StatusCreated {
		server.setLaunchCookieValue(writer, receipt.Created.LaunchID, receipt.Created.Capability)
	}
	writer.Header().Set("Content-Type", "application/json")
	if receipt.Replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.WriteHeader(receipt.Status)
	_, _ = writer.Write(receipt.Body)
}

func (server *Server) productCreationError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, launchservice.ErrIdempotencyKeyReused) {
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
		return
	}
	code := "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
	switch {
	case errors.Is(err, launch.ErrDOSEntryMissing):
		code = "LAUNCH_DOS_ENTRY_MISSING"
	case errors.Is(err, launch.ErrDOSEntryUnsafe):
		code = "LAUNCH_DOS_ENTRY_UNSAFE"
	case errors.Is(err, launch.ErrSaveIncompatible):
		code = "LAUNCH_SAVE_INCOMPATIBLE"
	case errors.Is(err, launch.ErrBlocked):
	default:
		server.databaseError(writer, request, err)
		return
	}
	writeError(writer, request, http.StatusUnprocessableEntity, "LAUNCH_BLOCKED", "当前游戏或核心无法启动",
		map[string]any{"blockers": []map[string]any{{"code": code, "level": "BLOCKING"}}})
}

func (server *Server) setLaunchCookie(writer http.ResponseWriter, launchID string) {
	parsed, err := uuid.Parse(launchID)
	if err != nil || parsed.Version() != 7 {
		return
	}
	capability := server.contentDeps.Credentials.Capability(parsed)
	server.setLaunchCookieValue(writer, launchID, retromruntime.EncodeCapability(capability))
}

func (server *Server) setLaunchCookieValue(
	writer http.ResponseWriter,
	launchID, encodedCapability string,
) {
	http.SetCookie(
		writer,
		&http.Cookie{
			Name:     "retrom_launch_" + launchID,
			Value:    encodedCapability,
			Path:     "/runtime/launches/" + launchID + "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   server.config.PublicOrigin.Scheme == "https",
		},
	)
	server.setLaunchContentGrant(writer, launchID, encodedCapability, 86400)
}

func (server *Server) createReviewPreview(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		ClientCapabilities   launch.Capabilities `json:"clientCapabilities"`
		RestoreFromPreviewID *string             `json:"restoreFromPreviewId"`
	}
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核预览请求无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	itemID := request.PathValue("importItemId")
	if err := server.importDeps.Importer.RefreshReviewPreviewValidation(request.Context(), itemID); err != nil {
		writeError(writer, request, http.StatusUnprocessableEntity,
			"REVIEW_PREVIEW_UNAVAILABLE", "无法刷新审核运行依赖", map[string]any{})
		return
	}
	created, err := server.playDeps.Launcher.CreateReviewPreview(request.Context(), launch.ReviewPreviewRequest{
		ImportItemID: request.PathValue("importItemId"), ActorUserID: principal.UserID,
		IdempotencyKey: key, ClientCapabilities: body.ClientCapabilities, RestoreFromPreviewID: body.RestoreFromPreviewID,
	})
	if err != nil {
		code, message := "REVIEW_PREVIEW_UNAVAILABLE", "当前审核来源无法组成可运行预览"
		if errors.Is(err, launch.ErrBlocked) {
			code, message = "REVIEW_PREVIEW_CLIENT_UNSUPPORTED", "当前浏览器不满足该核心的运行要求"
		}
		writeError(writer, request, http.StatusUnprocessableEntity, code, message, map[string]any{
			"bestEffort": true,
		})
		return
	}
	server.setLaunchCookie(writer, created.PreviewID)
	writeJSON(writer, http.StatusCreated, created)
}

func (server *Server) launchCapability(request *http.Request) string {
	cookie, err := request.Cookie("retrom_launch_" + request.PathValue("launchId"))
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (server *Server) launchConfig(writer http.ResponseWriter, request *http.Request) {
	capability := server.launchCapability(request)
	configuration, err := server.playDeps.Launcher.Config(
		request.Context(),
		request.PathValue("launchId"),
		capability,
	)
	productErr := err
	if errors.Is(err, launch.ErrCredential) {
		configuration, err = server.playDeps.Launcher.ReviewPreviewConfig(
			request.Context(), request.PathValue("launchId"), capability,
		)
	}
	if err != nil {
		slog.WarnContext(request.Context(), "launch configuration unavailable",
			"launchId", request.PathValue("launchId"), "productError", productErr, "previewError", err)
		if !errors.Is(err, launch.ErrCredential) {
			server.databaseError(writer, request, err)
			return
		}
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if dimensions, dimensionErr := server.playDeps.Launcher.MultiDiscTelemetryDimensions(
		request.Context(), request.PathValue("launchId"), capability,
	); dimensionErr == nil {
		logMultiDiscRuntime(
			request.Context(), request.PathValue("launchId"), dimensions.PlatformKey,
			dimensions.TargetKey, dimensions.BundleDigest, dimensions.DiscCount,
			"kind", "launch", "resultCode", "OK",
		)
	}
	server.setLaunchContentGrant(
		writer, request.PathValue("launchId"), capability, 86400,
	)
	writer.Header().Set("Vary", "Cookie")
	writeJSON(writer, http.StatusOK, configuration)
}

func (server *Server) launchProgress(writer http.ResponseWriter, request *http.Request) {
	var body launch.PlaySnapshot
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游玩进度无效", map[string]any{})
		return
	}
	result, err := server.playDeps.Launcher.RecordPlaySnapshot(request.Context(), request.PathValue("launchId"),
		server.launchCapability(request), body)
	if err != nil {
		if errors.Is(err, launch.ErrCredential) {
			writeError(
				writer,
				request,
				http.StatusUnauthorized,
				"LAUNCH_CREDENTIAL_INVALID",
				"启动会话不可用",
				map[string]any{},
			)
			return
		}
		if errors.Is(err, launch.ErrBlocked) {
			writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游玩进度无效", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) launchFinish(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("launchId")
	err := server.playDeps.Launcher.FinishReviewPreview(request.Context(), id, server.launchCapability(request))
	if err != nil {
		if errors.Is(err, launch.ErrCredential) {
			writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "试玩会话不可用", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: "retrom_launch_" + id, Value: "", Path: "/runtime/launches/" + id + "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: server.config.PublicOrigin.Scheme == "https",
	})
	server.setLaunchContentGrant(writer, id, "", -1)
	writer.WriteHeader(http.StatusNoContent)
}

func validIdempotencyKey(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == strings.ToLower(value) && (parsed.Version() == 4 || parsed.Version() == 7)
}

func (server *Server) createSaveState(writer http.ResponseWriter, request *http.Request) {
	if server.rejectInvalidSaveSession(writer, request) {
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	if request.ContentLength > saves.MaxRequestBytes {
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"REQUEST_TOO_LARGE",
			"存档内容超过限制",
			map[string]any{},
		)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, saves.MaxRequestBytes)
	result, replayed, err := server.playDeps.Saves.CreateManual(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		key,
		saves.ManualUpload{ContentType: request.Header.Get("Content-Type"), Body: request.Body},
	)
	writeSaveStateResult(writer, request, result, replayed, err)
}

func writeSaveStateResult(
	writer http.ResponseWriter, request *http.Request, result saves.ManualResult, replayed bool, err error,
) {
	switch {
	case errors.Is(err, saves.ErrCredential):
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
	case errors.Is(err, saves.ErrTooLarge):
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"REQUEST_TOO_LARGE",
			"存档内容超过限制",
			map[string]any{},
		)
	case errors.Is(err, saves.ErrSyncConflict):
		writeError(
			writer,
			request,
			http.StatusConflict,
			"SAVE_SYNC_CONFLICT",
			"存档已被其他会话更新或删除，请重新从存档启动",
			map[string]any{},
		)
	case errors.Is(err, saves.ErrSequenceReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case errors.Is(err, saves.ErrCheckpointUnavailable):
		writeError(writer, request, http.StatusConflict, "RPG_CHECKPOINT_UNAVAILABLE", "当前状态不能创建检查点", map[string]any{})
	case errors.Is(err, saves.ErrCheckpointInvalid):
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"RPG_CHECKPOINT_INVALID",
			"检查点内容无效",
			map[string]any{},
		)
	case err != nil:
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "存档请求无效", map[string]any{})
	default:
		if replayed {
			writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
		}
		writeJSON(writer, http.StatusCreated, result)
	}
}

func (server *Server) checkpointStatus(writer http.ResponseWriter, request *http.Request) {
	if server.rejectInvalidSaveSession(writer, request) {
		return
	}
	result, err := server.playDeps.Saves.CheckpointStatus(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request),
	)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) rejectInvalidSaveSession(writer http.ResponseWriter, request *http.Request) bool {
	err := server.playDeps.Launcher.AuthorizeSave(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return true
	}
	return false
}

func (server *Server) launchState(writer http.ResponseWriter, request *http.Request) {
	if server.rejectInvalidSaveSession(writer, request) {
		return
	}
	if rejectMultipleRanges(writer, request) {
		return
	}
	digest, err := server.playDeps.Saves.StateFile(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if errors.Is(err, saves.ErrCheckpointIncompatible) {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"RPG_CHECKPOINT_INCOMPATIBLE",
			"存档与当前启动绑定不兼容",
			map[string]any{},
		)
		return
	}
	if errors.Is(err, saves.ErrCheckpointInvalid) {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"RPG_CHECKPOINT_INVALID",
			"检查点内容无效",
			map[string]any{},
		)
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "LAUNCH_CONTENT_NOT_FOUND", "启动内容不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest.FileRecord, digest.Digest, "application/octet-stream", true)
}

func (server *Server) serveBlob(
	writer http.ResponseWriter,
	request *http.Request,
	id, digest, mediaType string,
	private bool,
) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	file, err := server.contentDeps.Files.OpenRecord(id)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "FILE_STORAGE_UNAVAILABLE", "内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	if private {
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Set("Vary", "Cookie")
	} else {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("ETag", `"sha256-`+digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "content", time.Unix(0, 0), file)
}

func rejectMultipleRanges(writer http.ResponseWriter, request *http.Request) bool {
	if strings.Contains(request.Header.Get("Range"), ",") {
		writeError(
			writer,
			request,
			http.StatusRequestedRangeNotSatisfiable,
			"MULTIPLE_RANGES_UNSUPPORTED",
			"一次只能请求一个字节范围",
			map[string]any{},
		)
		return true
	}
	return false
}
