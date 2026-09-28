package httpapi

import (
	"errors"
	"mime"
	"net/http"

	"retrom/internal/mediaasset"
	libraryservice "retrom/internal/service/libraryimport"
)

func (server *Server) storeReviewScreenshot(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/png" && mediaType != "image/jpeg" {
		writeError(
			writer, request, http.StatusBadRequest, "REVIEW_SCREENSHOT_INVALID",
			"运行截图必须是 PNG 或 JPEG", map[string]any{},
		)
		return
	}
	body := http.MaxBytesReader(writer, request.Body, mediaasset.MaxImageBytes+1)
	result, err := server.reviewDeps.Screenshots.Store(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request), body,
	)
	if err != nil {
		if errors.Is(err, libraryservice.ErrPreviewCredential) {
			writeError(
				writer,
				request,
				http.StatusUnauthorized,
				"LAUNCH_CREDENTIAL_INVALID",
				"审核预览会话不可用",
				map[string]any{},
			)
			return
		}
		if errors.Is(err, libraryservice.ErrReviewScreenshotInvalid) {
			writeError(
				writer,
				request,
				http.StatusBadRequest,
				"REVIEW_SCREENSHOT_INVALID",
				"运行截图无效或超过大小限制",
				map[string]any{},
			)
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"screenshotId": result.ID, "importItemId": result.ImportItemID,
		"validationId": result.ValidationID, "providerId": result.ProviderID,
		"targetId": result.TargetID,
		"widthPx":  result.WidthPX, "heightPx": result.HeightPX,
		"capturedAtMs": result.CapturedAtMS, "url": "/api/v1/admin/review-assets/" + result.ID,
	})
}
