package httpapi

import (
	"errors"
	"fmt"
	"mime"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/launch"
	"retrom/internal/mediaasset"
	"retrom/internal/rpgmaker/runtimevalidation"
)

type rpgRuntimeLaunchRequest struct {
	ClientCapabilities launch.Capabilities `json:"clientCapabilities"`
}

func (server *Server) createRPGRuntimeValidation(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireRPGValidationWrite(writer, request)
	if !ok {
		return
	}
	var body rpgRuntimeLaunchRequest
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "运行验证请求无效", map[string]any{})
		return
	}
	itemID := request.PathValue("importItemId")
	binding, err := server.rpgValidations.Create(request.Context(), itemID, version)
	if err != nil {
		server.writeRPGValidationError(writer, request, err)
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	created, err := server.launcher.CreateRPGValidation(
		request.Context(), principal.ProfileID, binding.ValidationID,
		"/admin/reviews/"+itemID, body.ClientCapabilities,
	)
	if err != nil {
		abortErr := server.rpgValidations.AbortCreated(
			request.Context(), binding.ValidationID, "RPG_RUNTIME_ROUTE_UNAVAILABLE",
		)
		if abortErr != nil {
			server.databaseError(writer, request, abortErr)
			return
		}
		server.writeRPGValidationError(writer, request, err)
		return
	}
	server.setLaunchCookie(writer, created.LaunchID)
	writer.Header().Set("Location", "/api/v1/admin/reviews/"+itemID+
		"/runtime-validations/"+binding.ValidationID)
	writeJSON(writer, http.StatusCreated, map[string]any{
		"validationId": binding.ValidationID,
		"launchId":     created.LaunchID,
		"playerUrl":    created.PlayURL,
		"expiresAtMs":  binding.ExpiresAtMS,
	})
}

func (server *Server) rpgRuntimeValidation(writer http.ResponseWriter, request *http.Request) {
	view, err := server.rpgValidations.Get(
		request.Context(), request.PathValue("importItemId"), request.PathValue("validationId"),
	)
	if err != nil {
		server.writeRPGValidationError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) createRPGRuntimeValidationRestore(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireRPGValidationWrite(writer, request)
	if !ok {
		return
	}
	var body rpgRuntimeLaunchRequest
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "恢复验证请求无效", map[string]any{})
		return
	}
	itemID, validationID := request.PathValue("importItemId"), request.PathValue("validationId")
	if err := server.rpgValidations.CurrentForRestore(
		request.Context(), itemID, validationID, version,
	); err != nil {
		server.writeRPGValidationError(writer, request, err)
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	created, err := server.launcher.CreateRPGValidationRestore(
		request.Context(), principal.ProfileID, validationID,
		"/admin/reviews/"+itemID, body.ClientCapabilities,
	)
	if err != nil {
		server.writeRPGValidationError(writer, request, err)
		return
	}
	server.setLaunchCookie(writer, created.LaunchID)
	writeJSON(writer, http.StatusCreated, map[string]any{
		"validationId": validationID,
		"launchId":     created.LaunchID,
		"playerUrl":    created.PlayURL,
		"expiresAtMs":  created.HardExpiresAtMS,
	})
}

func (server *Server) decideRPGRuntimeValidation(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireRPGValidationWrite(writer, request)
	if !ok {
		return
	}
	var body struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "运行验证决定无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	view, err := server.rpgValidations.Decide(
		request.Context(), request.PathValue("importItemId"), request.PathValue("validationId"),
		version, principal.UserID, body.Decision, body.Note,
	)
	if err != nil {
		server.writeRPGValidationError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) rpgMakerGateEvent(writer http.ResponseWriter, request *http.Request) {
	var body runtimevalidation.GateRequest
	if decodeJSON(writer, request, &body, 64<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "运行验证事件无效", map[string]any{})
		return
	}
	accepted, err := server.rpgValidations.ApplyGate(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request), body,
	)
	if err != nil {
		server.writeRPGValidationError(writer, request, err)
		return
	}
	if accepted.IdempotentReplay {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writeJSON(writer, http.StatusOK, accepted)
}

func requireRPGValidationWrite(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return 0, false
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return 0, false
	}
	return version, true
}

func (server *Server) writeRPGValidationError(
	writer http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, runtimevalidation.ErrNotFound):
		writeError(writer, request, http.StatusNotFound, "RPG_RUNTIME_VALIDATION_NOT_FOUND", "运行验证不存在", map[string]any{})
	case errors.Is(err, runtimevalidation.ErrVersion):
		writeError(
			writer, request, http.StatusConflict, "RPG_RUNTIME_VALIDATION_VERSION_CONFLICT",
			"审核条目版本已变化", map[string]any{},
		)
	case errors.Is(err, runtimevalidation.ErrBindingStale):
		writeError(writer, request, http.StatusConflict, "RPG_RUNTIME_CONTENT_MISMATCH", "运行验证输入已变化", map[string]any{})
	case errors.Is(err, runtimevalidation.ErrCredential), errors.Is(err, launch.ErrCredential):
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
	case errors.Is(err, runtimevalidation.ErrProtocol):
		writeError(writer, request, http.StatusConflict, "RPG_RUNTIME_PROTOCOL_VIOLATION", "运行验证事件违反协议", map[string]any{})
	case errors.Is(err, runtimevalidation.ErrDecision):
		writeError(
			writer, request, http.StatusUnprocessableEntity, "RPG_RUNTIME_VALIDATION_DECISION_INVALID",
			"运行验证决定无效", map[string]any{},
		)
	case errors.Is(err, runtimevalidation.ErrInvalidState):
		writeError(writer, request, http.StatusConflict, "RPG_RUNTIME_INVALID_STATE", "运行验证状态不允许该操作", map[string]any{})
	case errors.Is(err, launch.ErrBlocked):
		writeError(writer, request, http.StatusConflict, "RPG_RUNTIME_ROUTE_UNAVAILABLE", "当前浏览器或运行构件无法启动", map[string]any{})
	default:
		server.databaseError(writer, request, fmt.Errorf("RPG runtime validation: %w", err))
	}
}

func (server *Server) storeRPGRuntimeScreenshot(
	writer http.ResponseWriter,
	request *http.Request,
) (bool, error) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return unhandledRPGRuntimeScreenshot()
	}
	if mediaType != "image/png" {
		return unhandledRPGRuntimeScreenshot()
	}
	result, err := server.rpgValidations.StoreRestoreScreenshot(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request),
		http.MaxBytesReader(writer, request.Body, mediaasset.MaxImageBytes+1),
	)
	if errors.Is(err, runtimevalidation.ErrCredential) {
		return unhandledRPGRuntimeScreenshot()
	}
	if err != nil {
		return true, fmt.Errorf("store RPG runtime screenshot: %w", err)
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"screenshotId": result.ValidationID,
		"importItemId": result.ImportItemID,
		"validationId": result.ValidationID,
		"widthPx":      result.WidthPX,
		"heightPx":     result.HeightPX,
		"capturedAtMs": result.CapturedAtMS,
		"url":          "/api/v1/admin/review-assets/" + result.ValidationID,
	})
	return true, nil
}

func unhandledRPGRuntimeScreenshot() (bool, error) {
	return false, nil
}
