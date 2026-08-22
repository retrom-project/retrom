package httpapi

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/dosbundle"
	"retrom/internal/launch"
	"retrom/internal/mediaasset"
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
	http.SetCookie(
		writer,
		&http.Cookie{
			Name:     "retrom_launch_" + launchID,
			Value:    retromruntime.EncodeCapability(capability),
			Path:     "/runtime/launches/" + launchID + "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   server.config.PublicOrigin.Scheme == "https",
		},
	)
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
	writer.Header().Set("Vary", "Cookie")
	writeJSON(writer, http.StatusOK, configuration)
}

func (server *Server) launchGame(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	content, err := server.runtimeContent(request)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动内容不可用", map[string]any{})
		return
	}
	isMultiDisc := content.Format == "RETROM_MULTIDISC_M3U_V1" && content.DiscCount >= 2
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	stat, err := file.Stat()
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(request.PathValue("logicalName")))
	if content.Format == "RETROM_MULTIDISC_M3U_V1" {
		mediaType = "audio/x-mpegurl; charset=utf-8"
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	body, etag, err := launchGameBody(file, stat.Size(), content)
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	writer.Header().Set("ETag", `"sha256-`+etag+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	metricsWriter := &multiDiscResponseWriter{ResponseWriter: writer}
	http.ServeContent(metricsWriter, request, request.PathValue("logicalName"), time.Unix(0, 0), body)
	if isMultiDisc {
		status := metricsWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		resultCode := "OK"
		if status >= http.StatusBadRequest {
			resultCode = "HTTP_ERROR"
		}
		logMultiDiscContentResponse(
			request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
			content.ArtifactVersion, content.DiscCount, "PLAYLIST", status, metricsWriter.bytes, resultCode,
		)
	}
}

func (server *Server) runtimeContent(request *http.Request) (launch.ContentView, error) {
	launchID := request.PathValue("launchId")
	capability := server.launchCapability(request)
	logicalName := request.PathValue("logicalName")
	content, err := server.launcher.Content(request.Context(), launchID, capability, logicalName)
	if err == nil {
		return content, nil
	}
	content, err = server.launcher.ReviewPreviewContent(request.Context(), launchID, capability, logicalName)
	if err != nil {
		return launch.ContentView{}, fmt.Errorf("review preview content: %w", err)
	}
	return content, nil
}

func launchGameBody(file io.ReadSeeker, size int64, content launch.ContentView) (io.ReadSeeker, string, error) {
	if content.Format != "RETROM_DOS_DIRECT_ZIP_V1" {
		return file, content.Digest, nil
	}
	if content.CoreID != "dosbox_pure" {
		return nil, "", fmt.Errorf("launch game body: %w", dosbundle.ErrInvalid)
	}
	var overlay *dosbundle.Overlay
	var err error
	readerAt, ok := file.(io.ReaderAt)
	if !ok {
		return nil, "", fmt.Errorf("launch game reader: %w", dosbundle.ErrInvalid)
	}
	if content.DOSEntry == nil {
		overlay, err = dosbundle.NewMenu(readerAt, size)
	} else {
		overlay, err = dosbundle.New(readerAt, size, *content.DOSEntry)
	}
	if err != nil {
		return nil, "", fmt.Errorf("build DOS overlay: %w", err)
	}
	digestInput := content.Format + "\x00" + content.Digest + "\x00" + nullableDOSEntry(content.DOSEntry)
	digest := sha256.Sum256([]byte(digestInput))
	return overlay, hex.EncodeToString(digest[:]), nil
}

func nullableDOSEntry(entry *string) string {
	if entry == nil {
		return "<menu>"
	}
	return *entry
}

func (server *Server) launchExternalFile(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	content, err := server.launcher.External(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		request.PathValue("logicalName"),
	)
	if err != nil {
		content, err = server.launcher.ReviewPreviewExternal(
			request.Context(), request.PathValue("launchId"), server.launchCapability(request),
			request.PathValue("logicalName"),
		)
	}
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动外部文件不可用", map[string]any{})
		return
	}
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		if content.Kind == "DISC" {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreKey,
				content.ArtifactVersion, content.DiscCount, "DISC", http.StatusUnauthorized, 0,
				"LAUNCH_CREDENTIAL_INVALID",
			)
		}
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动外部文件不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+content.Digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	metricsWriter := &multiDiscResponseWriter{ResponseWriter: writer}
	http.ServeContent(metricsWriter, request, request.PathValue("logicalName"), time.Unix(0, 0), file)
	if content.Kind == "DISC" {
		status := metricsWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		resultCode := "OK"
		if status >= http.StatusBadRequest {
			resultCode = "HTTP_ERROR"
		}
		logMultiDiscContentResponse(
			request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreKey,
			content.ArtifactVersion, content.DiscCount, "DISC", status, metricsWriter.bytes, resultCode,
		)
	}
}

func (server *Server) launchBIOSBundle(writer http.ResponseWriter, request *http.Request) {
	server.launchBundle(writer, request, "BIOS_BUNDLE")
}

func (server *Server) launchParentBundle(writer http.ResponseWriter, request *http.Request) {
	server.launchBundle(writer, request, "PARENT")
}

func (server *Server) populateLaunchBundle(
	archiveWriter *zip.Writer,
	files []launch.BundleFile,
) (string, string) {
	for _, entry := range files {
		if entry.LogicalName == "" || filepath.Base(entry.LogicalName) != entry.LogicalName ||
			strings.Contains(entry.LogicalName, "\\") {
			return "LAUNCH_DEPENDENCY_INVALID", "启动依赖清单无效"
		}
		destination, err := archiveWriter.CreateHeader(deterministicStoreZIPHeader(entry.LogicalName))
		if err != nil {
			return "CAS_UNAVAILABLE", "无法装配启动依赖"
		}
		source, err := server.blobs.OpenDigest(entry.SHA256)
		if err != nil {
			return "CAS_UNAVAILABLE", "启动依赖不可用"
		}
		_, copyErr := io.Copy(destination, source)
		cleanup.Error("close", source.Close())
		if copyErr != nil {
			return "CAS_UNAVAILABLE", "无法读取启动依赖"
		}
	}
	return "", ""
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) launchBundle(writer http.ResponseWriter, request *http.Request, kind string) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	files, err := server.launcher.BundleFiles(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		kind,
	)
	if err != nil {
		files, err = server.launcher.ReviewPreviewBundleFiles(
			request.Context(), request.PathValue("launchId"), server.launchCapability(request), kind,
		)
	}
	if errors.Is(err, launch.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if len(files) == 0 {
		writeError(writer, request, http.StatusNotFound, "LAUNCH_CONTENT_NOT_FOUND", "启动依赖不存在", map[string]any{})
		return
	}
	temporary, err := os.CreateTemp(filepath.Join(server.config.DataDir, "tmp", "jobs"), ".launch-bundle-")
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法装配启动依赖", map[string]any{})
		return
	}
	temporaryPath := temporary.Name()
	defer cleanup.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法装配启动依赖", map[string]any{})
		return
	}
	archiveWriter := zip.NewWriter(temporary)
	if errorCode, errorMessage := server.populateLaunchBundle(archiveWriter, files); errorCode != "" {
		cleanup.Error("close", archiveWriter.Close())
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, errorCode, errorMessage, map[string]any{})
		return
	}
	if err := archiveWriter.Close(); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法完成启动依赖", map[string]any{})
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法读取启动依赖", map[string]any{})
		return
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, temporary); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法校验启动依赖", map[string]any{})
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法读取启动依赖", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", temporary.Close()) }()
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest.Sum(nil))+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "bundle.zip", time.Unix(0, 0), temporary)
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
	if request.ContentLength > (75 << 20) {
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"SAVE_STATE_TOO_LARGE",
			"存档内容超过限制",
			map[string]any{},
		)
		return
	}
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
			"SAVE_STATE_TOO_LARGE",
			"存档内容超过限制",
			map[string]any{},
		)
	case errors.Is(err, saves.ErrSequenceReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case err != nil:
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "存档请求无效", map[string]any{})
	default:
		if replayed {
			writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
		}
		writeJSON(writer, http.StatusCreated, result)
	}
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

func deterministicStoreZIPHeader(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	// archive/zip otherwise emits an extended-timestamp extra field. These DOS
	// fields are the only public API for the exact empty-Extra 1980 header.
	header.ModifiedDate = 33 //nolint:staticcheck // Required by RETROM_EJS_DEP_ZIP_V1.
	header.ModifiedTime = 0  //nolint:staticcheck // Required by RETROM_EJS_DEP_ZIP_V1.
	return header
}
