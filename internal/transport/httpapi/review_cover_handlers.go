package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	libraryimportmodel "retrom/internal/model/libraryimport"
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
	if decodeJSON(writer, request, &body, 8<<10) != nil || body.Kind != "COVER" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核封面参数无效", map[string]any{})
		return
	}
	result, err := server.reviewCoverUploads.Upload(request.Context(), libraryimportmodel.ReviewCoverRequest{
		ItemID: request.PathValue("importItemId"), UploadFileID: body.UploadFileID,
		Kind: body.Kind, ExpectedVersion: expected,
	})
	if err != nil {
		server.reviewCoverError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusCreated, struct {
		libraryimportmodel.ReviewCoverResult

		URL string `json:"url"`
	}{ReviewCoverResult: result, URL: "/api/v1/admin/review-assets/" + result.AssetID})
}

func (server *Server) reviewCoverError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, libraryimportmodel.ErrReviewCoverUploadInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "ASSET_UPLOAD_INVALID", "上传文件不可用", map[string]any{})
	case errors.Is(err, libraryimportmodel.ErrReviewCoverCASUnavailable):
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "媒体字节不可用", map[string]any{})
	case errors.Is(err, libraryimportmodel.ErrReviewCoverImageInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity,
			"ASSET_IMAGE_INVALID", "封面必须是受限 PNG、JPEG 或 WebP", map[string]any{})
	case errors.Is(err, libraryimportmodel.ErrReviewCoverVersion):
		writeError(writer, request, http.StatusConflict, "REVIEW_VERSION_CONFLICT", "审核条目已发生变化", map[string]any{})
	case errors.Is(err, libraryimportmodel.ErrReviewCoverConsumed):
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
	default:
		server.databaseError(writer, request, err)
	}
}
