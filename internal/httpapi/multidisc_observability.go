package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"retrom/internal/httpapi/generated"
	"retrom/internal/launch"
)

const multiDiscRuntimeEventName = "retrom.multidisc.runtime"

type multiDiscResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (writer *multiDiscResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *multiDiscResponseWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	written, err := writer.ResponseWriter.Write(value)
	writer.bytes += int64(written)
	if err != nil {
		return written, fmt.Errorf("httpapi/multi-disc response: %w", err)
	}
	return written, nil
}

func discCountBucket(count int) string {
	switch {
	case count < 0:
		return "unknown"
	case count < 2:
		return "0-1"
	case count == 2:
		return "2"
	case count <= 4:
		return "3-4"
	case count <= 8:
		return "5-8"
	default:
		return "9+"
	}
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func logMultiDiscRuntime(
	ctx context.Context,
	launchID, platformKey, targetKey, bundleDigest string,
	discCount int,
	attributes ...any,
) {
	base := make([]any, 0, 14+len(attributes))
	base = append(base,
		"event", multiDiscRuntimeEventName,
		"requestId", requestID(ctx),
		"launchId", launchID,
		"platformKey", platformKey,
		"targetKey", targetKey,
		"bundleSha256", bundleDigest,
		"discCountBucket", discCountBucket(discCount),
	)
	slog.InfoContext(ctx, "multi-disc runtime event", append(base, attributes...)...)
}

func validMultiDiscPlayerEvent(
	body generated.MultiDiscPlayerEventRequest,
	dimensions launch.MultiDiscTelemetryDimensions,
) bool {
	if body.DiscCount != dimensions.DiscCount {
		return false
	}
	eventType, resultCode := string(body.EventType), string(body.ResultCode)
	switch eventType {
	case "START", "SWITCH_SUCCESS", "SAVE_RESTORE_SUCCESS":
		return resultCode == "OK" && body.ObservedDiscCount != nil &&
			*body.ObservedDiscCount == dimensions.DiscCount
	case "DISK_COUNT_MISMATCH":
		return resultCode == "PLAYER_DISC_SET_INVALID" && body.ObservedDiscCount != nil &&
			*body.ObservedDiscCount != dimensions.DiscCount
	case "SWITCH_FAILURE", "SAVE_RESTORE_FAILURE":
		return resultCode != "OK"
	default:
		return false
	}
}

func (server *Server) multiDiscPlayerEvent(writer http.ResponseWriter, request *http.Request) {
	var body generated.MultiDiscPlayerEventRequest
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "Player 观测事件无效", map[string]any{})
		return
	}
	dimensions, err := server.launcher.MultiDiscTelemetryDimensions(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if !validMultiDiscPlayerEvent(body, dimensions) {
		writeError(
			writer, request, http.StatusUnprocessableEntity, "PLAYER_EVENT_INVALID",
			"Player 观测事件与启动配置不一致", map[string]any{},
		)
		return
	}
	observedBucket := "unknown"
	if body.ObservedDiscCount != nil {
		observedBucket = discCountBucket(*body.ObservedDiscCount)
	}
	logMultiDiscRuntime(
		request.Context(), request.PathValue("launchId"), dimensions.PlatformKey, dimensions.TargetKey,
		dimensions.BundleDigest, dimensions.DiscCount,
		"kind", "player",
		"playerEvent", string(body.EventType),
		"resultCode", string(body.ResultCode),
		"observedDiscCountBucket", observedBucket,
	)
	writer.WriteHeader(http.StatusNoContent)
}

func logMultiDiscContentResponse(
	ctx context.Context,
	launchID, platformKey, targetKey, bundleDigest string,
	discCount int,
	contentRole string,
	status int,
	bytes int64,
	resultCode string,
) {
	logMultiDiscRuntime(
		ctx, launchID, platformKey, targetKey, bundleDigest, discCount,
		"kind", "content",
		"contentRole", contentRole,
		"httpStatus", strconv.Itoa(status),
		"responseBytes", bytes,
		"resultCode", resultCode,
	)
}
