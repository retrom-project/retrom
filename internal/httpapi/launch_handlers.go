package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/launch"
	"retrom/internal/mediaasset"
	"retrom/internal/rpgmaker/runtimevalidation"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/saves"
)

func (server *Server) writeStoredLaunchResponse(
	writer http.ResponseWriter,
	request *http.Request,
	requestDigest, storedDigest string,
	storedStatus int,
	storedBody []byte,
) {
	if subtle.ConstantTimeCompare([]byte(storedDigest), []byte(requestDigest)) != 1 {
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
		return
	}
	if storedStatus == http.StatusCreated {
		var replay struct {
			LaunchID string `json:"launchId"`
		}
		if json.Unmarshal(storedBody, &replay) == nil {
			server.setLaunchCookie(writer, replay.LaunchID)
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	writer.WriteHeader(storedStatus)
	_, _ = writer.Write(storedBody)
}

// Contract branches stay contiguous for a single auditable decision.
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
	requestDigest := hex.EncodeToString(digestBytes[:])
	server.idempotency.Lock()
	defer server.idempotency.Unlock()
	var storedDigest string
	var storedStatus int
	var storedBody []byte
	err := server.database.QueryRowContext(request.Context(), `
SELECT request_digest,
http_status,
response_body
FROM idempotency_records
WHERE operation_id='postLaunch'
AND key=?
AND principal_id=?
`, key, principal.UserID).
		Scan(&storedDigest, &storedStatus, &storedBody)
	if err == nil {
		server.writeStoredLaunchResponse(writer, request, requestDigest, storedDigest, storedStatus, storedBody)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		server.databaseError(writer, request, err)
		return
	}
	created, err := server.launcher.Create(request.Context(), principal.ProfileID, body)
	if err != nil {
		code := "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
		if errors.Is(err, launch.ErrDOSEntryMissing) {
			code = "LAUNCH_DOS_ENTRY_MISSING"
		}
		if errors.Is(err, launch.ErrDOSEntryUnsafe) {
			code = "LAUNCH_DOS_ENTRY_UNSAFE"
		}
		if errors.Is(err, launch.ErrSaveIncompatible) {
			code = "LAUNCH_SAVE_INCOMPATIBLE"
		}
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"LAUNCH_BLOCKED",
			"当前游戏或核心无法启动",
			map[string]any{"blockers": []map[string]any{{"code": code, "level": "BLOCKING"}}},
		)
		return
	}
	status := http.StatusCreated
	responseValue := any(created)
	if created.Status == "VALIDATION_PENDING" {
		status = http.StatusAccepted
		responseValue = map[string]any{
			"status":       created.Status,
			"jobId":        created.JobID,
			"retryAfterMs": created.RetryAfterMS,
		}
	} else {
		server.setLaunchCookie(writer, created.LaunchID)
	}
	responseBody, _ := json.Marshal(responseValue)
	now := server.now().UnixMilli()
	if _, err := server.database.ExecContext(request.Context(), `
INSERT INTO idempotency_records(principal_id,
operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms) VALUES(?,
'postLaunch',
?,
?,
?,
'{}',
?,
?,
?)
`,
		principal.UserID,
		key,
		requestDigest,
		status,
		responseBody,
		now,
		now+int64(24*time.Hour/time.Millisecond),
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(responseBody)
}

func (server *Server) setLaunchCookie(writer http.ResponseWriter, launchID string) {
	parsed, err := uuid.Parse(launchID)
	if err != nil || parsed.Version() != 7 {
		return
	}
	capability := server.credentials.Capability(parsed)
	encodedCapability := retromruntime.EncodeCapability(capability)
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
		ClientCapabilities launch.Capabilities `json:"clientCapabilities"`
	}
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核预览请求无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	created, err := server.launcher.CreateReviewPreview(request.Context(), launch.ReviewPreviewRequest{
		ImportItemID: request.PathValue("importItemId"), ActorUserID: principal.UserID,
		IdempotencyKey: key, ClientCapabilities: body.ClientCapabilities,
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

func (server *Server) storeReviewScreenshot(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/png" {
		writeError(writer, request, http.StatusBadRequest, "REVIEW_SCREENSHOT_INVALID", "运行截图必须是 PNG", map[string]any{})
		return
	}
	if handled, rpgErr := server.storeRPGRuntimeScreenshot(writer, request); handled {
		if rpgErr != nil {
			if errors.Is(rpgErr, runtimevalidation.ErrImageInvalid) {
				writeError(
					writer, request, http.StatusUnprocessableEntity, "RPG_RUNTIME_SCREENSHOT_INVALID",
					"恢复截图无效或超过大小限制", map[string]any{},
				)
				return
			}
			server.writeRPGValidationError(writer, request, rpgErr)
		}
		return
	}
	body := http.MaxBytesReader(writer, request.Body, mediaasset.MaxImageBytes+1)
	result, err := server.launcher.StoreReviewScreenshot(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request), body,
	)
	if err != nil {
		if errors.Is(err, launch.ErrCredential) {
			writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "审核预览会话不可用", map[string]any{})
			return
		}
		if errors.Is(err, launch.ErrReviewCaptureNotAllowed) {
			writeError(
				writer, request, http.StatusConflict, "REVIEW_CAPTURE_NOT_ALLOWED",
				"只有运行检查通过的条目才能保存五秒截图", map[string]any{},
			)
			return
		}
		if errors.Is(err, launch.ErrReviewScreenshotInvalid) {
			writeError(writer, request, http.StatusBadRequest, "REVIEW_SCREENSHOT_INVALID", "运行截图无效或超过大小限制", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"screenshotId":   result.ID,
		"importItemId":   result.ImportItemID,
		"validationId":   result.ValidationID,
		"coreArtifactId": result.CoreArtifactID,
		"widthPx":        result.WidthPX,
		"heightPx":       result.HeightPX,
		"capturedAtMs":   result.CapturedAtMS,
		"url":            "/api/v1/admin/review-assets/" + result.ID,
	})
}

func (server *Server) launchConfig(writer http.ResponseWriter, request *http.Request) {
	capability := server.launchCapability(request)
	configuration, err := server.launcher.Config(
		request.Context(),
		request.PathValue("launchId"),
		capability,
	)
	if err != nil {
		configuration, err = server.launcher.ReviewPreviewConfig(
			request.Context(), request.PathValue("launchId"), capability,
		)
	}
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if configuration.DiscSet != nil {
		if dimensions, dimensionErr := server.launcher.MultiDiscTelemetryDimensions(
			request.Context(), request.PathValue("launchId"), capability,
		); dimensionErr == nil {
			logMultiDiscRuntime(
				request.Context(), request.PathValue("launchId"), dimensions.PlatformKey,
				dimensions.CoreKey, dimensions.ArtifactVersion, dimensions.DiscCount,
				"kind", "launch", "resultCode", "OK",
			)
		}
	}
	server.setLaunchContentGrant(
		writer, request.PathValue("launchId"), capability, 86400,
	)
	if launchUsesProjectFiles(configuration) {
		server.setLaunchProjectGrant(
			writer, request.PathValue("launchId"), capability, 86400,
		)
	}
	writer.Header().Set("Vary", "Cookie")
	writeJSON(writer, http.StatusOK, configuration)
}

func launchUsesProjectFiles(configuration launch.Config) bool {
	return configuration.RPGMaker != nil || configuration.ONS != nil || configuration.KiriKiri != nil
}

func (server *Server) launchStart(writer http.ResponseWriter, request *http.Request) {
	server.recordPlay(writer, request, "start")
}

func (server *Server) launchHeartbeat(writer http.ResponseWriter, request *http.Request) {
	server.recordPlay(writer, request, "heartbeat")
}

func (server *Server) launchFinish(writer http.ResponseWriter, request *http.Request) {
	server.recordPlay(writer, request, "finish")
}

func (server *Server) recordPlay(writer http.ResponseWriter, request *http.Request, kind string) {
	var body launch.PlayEvent
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游玩事件无效", map[string]any{})
		return
	}
	result, err := server.launcher.RecordPlay(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		kind,
		body,
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "PLAY_SEQUENCE_GAP", "游玩事件序号或会话状态无效", map[string]any{})
		return
	}
	if kind == "finish" {
		http.SetCookie(
			writer,
			&http.Cookie{
				Name:     "retrom_launch_" + request.PathValue("launchId"),
				Value:    "",
				Path:     "/runtime/launches/" + request.PathValue("launchId") + "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   server.config.PublicOrigin.Scheme == "https",
			},
		)
		server.setLaunchContentGrant(writer, request.PathValue("launchId"), "", -1)
		server.setLaunchProjectGrant(writer, request.PathValue("launchId"), "", -1)
	}
	writeJSON(writer, http.StatusOK, result)
}

func validIdempotencyKey(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == strings.ToLower(value) && (parsed.Version() == 4 || parsed.Version() == 7)
}

func (server *Server) createSaveState(writer http.ResponseWriter, request *http.Request) {
	if server.rejectNetplaySave(writer, request) {
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
	result, replayed, err := server.saveService.CreateManual(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		key,
		request,
	)
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
	case errors.Is(err, saves.ErrSequenceReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case errors.Is(err, saves.ErrCheckpointUnavailable):
		writeError(writer, request, http.StatusConflict, "RPG_CHECKPOINT_UNAVAILABLE", "当前状态不能创建检查点", map[string]any{})
	case errors.Is(err, saves.ErrCheckpointInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "RPG_CHECKPOINT_INVALID", "检查点内容无效", map[string]any{})
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
	if server.rejectNetplaySave(writer, request) {
		return
	}
	result, err := server.saveService.CheckpointStatus(
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

func (server *Server) rejectNetplaySave(writer http.ResponseWriter, request *http.Request) bool {
	access, err := server.launcher.SaveAccess(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return true
	}
	if access == "NETPLAY_DISABLED" {
		writeError(writer, request, http.StatusConflict, "NETPLAY_SAVE_UNSUPPORTED", "联机模式不支持存档", map[string]any{})
		return true
	}
	return false
}

func (server *Server) launchState(writer http.ResponseWriter, request *http.Request) {
	if server.rejectNetplaySave(writer, request) {
		return
	}
	if rejectMultipleRanges(writer, request) {
		return
	}
	digest, err := server.saveService.StateDigest(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if errors.Is(err, saves.ErrCheckpointIncompatible) {
		writeError(writer, request, http.StatusConflict, "RPG_CHECKPOINT_INCOMPATIBLE", "存档与当前启动绑定不兼容", map[string]any{})
		return
	}
	if errors.Is(err, saves.ErrCheckpointInvalid) {
		writeError(writer, request, http.StatusUnprocessableEntity, "RPG_CHECKPOINT_INVALID", "检查点内容无效", map[string]any{})
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "LAUNCH_CONTENT_NOT_FOUND", "启动内容不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest, "application/octet-stream", true)
}

func (server *Server) serveBlob(
	writer http.ResponseWriter,
	request *http.Request,
	digest, mediaType string,
	private bool,
) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "内容不可用", map[string]any{})
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
