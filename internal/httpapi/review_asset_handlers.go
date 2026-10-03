package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	libraryservice "retrom/internal/service/libraryimport"
)

func (server *Server) createReviewAsset(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		UploadFileID string `json:"uploadFileId"`
		Kind         string `json:"kind"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil || (body.Kind != "COVER" && body.Kind != "VIDEO") {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核媒体参数无效", map[string]any{})
		return
	}
	result, err := server.reviewDeps.AssetUploads.Upload(request.Context(), libraryservice.ReviewAssetRequest{
		ItemID: request.PathValue("importItemId"), UploadFileID: body.UploadFileID,
		Kind: body.Kind, ExpectedVersion: expected,
	})
	if err != nil {
		server.reviewAssetError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusCreated, struct {
		libraryservice.ReviewAssetResult
		URL string `json:"url"`
	}{ReviewAssetResult: result, URL: "/api/v1/admin/review-assets/" + result.AssetID})
}

func (server *Server) reviewAssetError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, libraryservice.ErrReviewAssetUploadInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "ASSET_UPLOAD_INVALID", "上传文件不可用", map[string]any{})
	case errors.Is(err, libraryservice.ErrReviewAssetStorageUnavailable):
		writeError(writer, request, http.StatusServiceUnavailable, "FILE_STORAGE_UNAVAILABLE", "媒体字节不可用", map[string]any{})
	case errors.Is(err, libraryservice.ErrReviewAssetVideoInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity,
			"ASSET_VIDEO_INVALID", "视频必须是 MP4 或 WebM，且不超过 256 MiB", map[string]any{})
	case errors.Is(err, libraryservice.ErrReviewAssetImageInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity,
			"ASSET_IMAGE_INVALID", "封面必须是受限 PNG、JPEG 或 WebP", map[string]any{})
	case errors.Is(err, libraryservice.ErrReviewAssetVersion):
		writeError(writer, request, http.StatusConflict, "REVIEW_VERSION_CONFLICT", "审核条目已发生变化", map[string]any{})
	case errors.Is(err, libraryservice.ErrReviewAssetConsumed):
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
	default:
		server.databaseError(writer, request, err)
	}
}
