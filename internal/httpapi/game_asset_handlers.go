package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/service/mediaaccess"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/payloadrelease"
)

type gameAssetUpload struct {
	UploadFileID string `json:"uploadFileId"`
	Kind         string `json:"kind"`
	Ordinal      int64  `json:"ordinal"`
}

type preparedGameAsset struct {
	uploadID, blobID, mediaType string
	width, height               *int64
}

func validGameAssetUpload(body gameAssetUpload) bool {
	validKind := body.Kind == "COVER" || body.Kind == "BACKGROUND" || body.Kind == "SCREENSHOT" ||
		body.Kind == "VIDEO"
	return validKind && body.Ordinal >= 0 && body.Ordinal <= 31 &&
		(body.Kind == "SCREENSHOT" || body.Ordinal == 0)
}

func (server *Server) prepareGameAsset(
	ctx context.Context,
	body gameAssetUpload,
) (preparedGameAsset, string, string) {
	var asset preparedGameAsset
	var digest string
	var blobSize int64
	if err := server.database.QueryRowContext(ctx, `
SELECT f.upload_session_id,
b.id,
b.sha256,
b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
AND f.state='COMPLETE'
`, body.UploadFileID).Scan(&asset.uploadID, &asset.blobID, &digest, &blobSize); err != nil {
		return preparedGameAsset{}, "ASSET_UPLOAD_INVALID", "上传文件不可用"
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		return preparedGameAsset{}, "CAS_UNAVAILABLE", "媒体字节不可用"
	}
	mediaType, width, height, errorCode, errorMessage := inspectUploadedGameAsset(file, blobSize, body.Kind)
	cleanup.Error("close", file.Close())
	asset.mediaType, asset.width, asset.height = mediaType, width, height
	return asset, errorCode, errorMessage
}

func (server *Server) readGameAssetUpload(
	writer http.ResponseWriter,
	request *http.Request,
) (gameAssetUpload, preparedGameAsset, bool) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return gameAssetUpload{}, preparedGameAsset{}, false
	}
	var body gameAssetUpload
	if decodeJSON(writer, request, &body, 8<<10) != nil || !validGameAssetUpload(body) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游戏媒体参数无效", map[string]any{})
		return gameAssetUpload{}, preparedGameAsset{}, false
	}
	asset, errorCode, errorMessage := server.prepareGameAsset(request.Context(), body)
	if errorCode != "" {
		status := http.StatusUnprocessableEntity
		if errorCode == "CAS_UNAVAILABLE" {
			status = http.StatusServiceUnavailable
		}
		writeError(writer, request, status, errorCode, errorMessage, map[string]any{})
		return gameAssetUpload{}, preparedGameAsset{}, false
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
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer dbexec.Rollback(transaction)
	version, err := currentGameAssetVersion(
		request.Context(), transaction, request.PathValue("gameId"),
	)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	now := server.now().UnixMilli()
	replacedBlobIDs, err := removeGameAssetSlot(
		request.Context(), transaction, request.PathValue("gameId"), body.Kind, body.Ordinal,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assetID, consumptionID := newUUIDString(), newUUIDString()
	if _, err := recordstore.CreateGameAssets(request.Context(), transaction, `
INSERT INTO game_assets(id,
game_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		assetID,
		request.PathValue("gameId"),
		asset.blobID,
		body.Kind,
		body.Ordinal,
		asset.width,
		asset.height,
		asset.mediaType,
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if _, err := recordstore.CreateUploadConsumptions(request.Context(), transaction, `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,
?,
?,
'GAME_ASSET',
?,
?)
`, consumptionID, asset.uploadID, body.UploadFileID, assetID, now); err != nil {
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
		return
	}
	result, err := recordstore.UpdateGames(request.Context(), transaction, recordstore.Update{
		Set: `
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=?
AND version=?
`,
			Args: []any{request.PathValue("gameId"), expected},
		},
		Values: []any{now},
	})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := server.payloadReleases.StageCandidates(request.Context(), transaction, replacedBlobIDs); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if _, err := payloadrelease.ScheduleConsumption(
		request.Context(), transaction, consumptionID, now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writeCreatedGameAsset(
		writer, request, asset, assetID, body.Kind, body.Ordinal, expected+1, now,
	)
}

func writeCreatedGameAsset(
	writer http.ResponseWriter,
	request *http.Request,
	asset preparedGameAsset,
	assetID, kind string,
	ordinal int64,
	version, createdAtMS int64,
) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(
		writer,
		http.StatusCreated,
		map[string]any{
			"assetId":     assetID,
			"gameId":      request.PathValue("gameId"),
			"kind":        kind,
			"ordinal":     ordinal,
			"widthPx":     asset.width,
			"heightPx":    asset.height,
			"mediaType":   asset.mediaType,
			"version":     version,
			"createdAtMs": createdAtMS,
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
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer dbexec.Rollback(transaction)
	version, err := currentGameAssetVersion(
		request.Context(), transaction, request.PathValue("gameId"),
	)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if !gameAssetExists(request.Context(), transaction, request.PathValue("gameId"), kind) {
		writeError(writer, request, http.StatusNotFound, "ASSET_NOT_FOUND", "媒体不存在", map[string]any{})
		return
	}
	now := server.now().UnixMilli()
	replacedBlobIDs, err := removeGameAssetSlot(
		request.Context(), transaction, request.PathValue("gameId"), kind, 0,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	result, err := recordstore.UpdateGames(request.Context(), transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{request.PathValue("gameId"), expected},
		},
		Values: []any{now},
	})
	if err != nil || rowsAffectedHTTP(result) != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := server.payloadReleases.StageCandidates(request.Context(), transaction, replacedBlobIDs); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	server.payloadReleases.Signal()
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.WriteHeader(http.StatusNoContent)
}

func currentGameAssetVersion(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
) (int64, error) {
	var version int64
	err := transaction.QueryRowContext(ctx, `
SELECT g.version
FROM games g
WHERE g.id=?
AND g.status='PUBLISHED'`, gameID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("httpapi/load current game asset version: %w", err)
	}
	return version, nil
}

func gameAssetExists(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, kind string,
) bool {
	var exists int
	return transaction.QueryRowContext(ctx, `
SELECT 1
FROM game_assets
WHERE game_id=?
AND kind=?
AND ordinal=0`, gameID, kind).Scan(&exists) == nil
}

func removeGameAssetSlot(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, kind string,
	ordinal int64,
) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT blob_id FROM game_assets WHERE game_id=? AND kind=? AND ordinal=? ORDER BY id
`, gameID, kind, ordinal)
	if err != nil {
		return nil, fmt.Errorf("httpapi/list replaced game assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	blobIDs := make([]string, 0, 1)
	for rows.Next() {
		var blobID string
		if err := rows.Scan(&blobID); err != nil {
			return nil, fmt.Errorf("httpapi/scan replaced game asset: %w", err)
		}
		blobIDs = append(blobIDs, blobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi/iterate replaced game assets: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM game_assets WHERE game_id=? AND kind=? AND ordinal=?
`, gameID, kind, ordinal); err != nil {
		return nil, fmt.Errorf("httpapi/delete replaced game asset: %w", err)
	}
	return blobIDs, nil
}

func rowsAffectedHTTP(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	value, _ := result.RowsAffected()
	return value
}

func newUUIDString() string {
	value, _ := uuid.NewV7()
	return value.String()
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
