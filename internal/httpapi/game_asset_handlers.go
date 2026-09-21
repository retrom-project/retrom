package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/authn"
	gameassets "retrom/internal/service/gameassets"
	"retrom/internal/service/mediaaccess"
)

type gameAssetUpload struct {
	UploadFileID string `json:"uploadFileId"`
	Kind         string `json:"kind"`
	Ordinal      int64  `json:"ordinal"`
}

func validGameAssetUpload(body gameAssetUpload) bool {
	return gameassets.ValidUpload(body.Kind, body.Ordinal)
}

func (server *Server) readGameAssetUpload(
	writer http.ResponseWriter,
	request *http.Request,
) (gameAssetUpload, gameassets.PreparedAsset, bool) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return gameAssetUpload{}, gameassets.PreparedAsset{}, false
	}
	var body gameAssetUpload
	if decodeJSON(writer, request, &body, 8<<10) != nil || !validGameAssetUpload(body) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏媒体参数无效", map[string]any{})
		return gameAssetUpload{}, gameassets.PreparedAsset{}, false
	}
	asset, err := server.gameAssets.Prepare(request.Context(), body.UploadFileID, body.Kind)
	if err != nil {
		var validation *gameassets.ValidationError
		if !errors.As(err, &validation) {
			validation = &gameassets.ValidationError{Code: "ASSET_UPLOAD_INVALID", Message: "上传文件不可用"}
		}
		status := http.StatusUnprocessableEntity
		if validation.Code == "CAS_UNAVAILABLE" {
			status = http.StatusServiceUnavailable
		}
		writeError(writer, request, status, validation.Code, validation.Message, map[string]any{})
		return gameAssetUpload{}, gameassets.PreparedAsset{}, false
	}
	return body, asset, true
}

// Asset upload branches are independent media, ownership, and optimistic-lock contract checks.
func (server *Server) createGameAsset(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	body, asset, ok := server.readGameAssetUpload(writer, request)
	if !ok {
		return
	}
	result, err := server.gameAssets.Create(request.Context(), gameassets.CreateRequest{
		GameID: request.PathValue("gameId"), UploadFileID: body.UploadFileID, Kind: body.Kind,
		Ordinal: body.Ordinal, ExpectedVersion: expected, NowMS: server.now().UnixMilli(), Asset: asset,
	})
	if errors.Is(err, gameassets.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if errors.Is(err, gameassets.ErrUploadConsumed) {
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writeCreatedGameAsset(writer, request, result)
}

func writeCreatedGameAsset(
	writer http.ResponseWriter,
	request *http.Request,
	result gameassets.CreateResult,
) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(
		writer,
		http.StatusCreated,
		map[string]any{
			"assetId":     result.AssetID,
			"gameId":      request.PathValue("gameId"),
			"kind":        result.Kind,
			"ordinal":     result.Ordinal,
			"widthPx":     result.WidthPX,
			"heightPx":    result.HeightPX,
			"mediaType":   result.MediaType,
			"version":     result.Version,
			"createdAtMs": result.CreatedAtMS,
		},
	)
}

func (server *Server) deleteGameAsset(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	kind := request.PathValue("assetKind")
	if kind != "VIDEO" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏媒体类型无效", map[string]any{})
		return
	}
	result, err := server.gameAssets.Delete(request.Context(), gameassets.DeleteRequest{
		GameID: request.PathValue("gameId"), Kind: kind, ExpectedVersion: expected, NowMS: server.now().UnixMilli(),
	})
	if errors.Is(err, gameassets.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if errors.Is(err, gameassets.ErrAssetNotFound) {
		writeError(writer, request, http.StatusNotFound, "ASSET_NOT_FOUND", "媒体不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) contentAsset(writer http.ResponseWriter, request *http.Request) {
	asset, err := server.mediaAccess.Game(request.Context(), request.PathValue("assetId"))
	if errors.Is(err, mediaaccess.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "ASSET_NOT_FOUND", "媒体不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.serveBlob(writer, request, asset.Digest, asset.MediaType, false)
}

func (server *Server) saveStateScreenshot(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	asset, err := server.mediaAccess.Save(request.Context(), request.PathValue("saveStateId"), principal.ProfileID)
	if errors.Is(err, mediaaccess.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "SAVE_SCREENSHOT_NOT_FOUND", "存档截图不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.serveBlob(writer, request, asset.Digest, asset.MediaType, true)
}
