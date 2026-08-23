package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/hasheous"
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

func insertGameAssetMetadataRevision(
	ctx context.Context,
	transaction *sql.Tx,
	revisionID, gameID string,
	metadata gameMetadata,
	now int64,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO game_metadata_revisions(
id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'ADMIN_EDIT',NULL,?)
`, revisionID, gameID, metadata.Title, metadata.Description, metadata.Developer, metadata.Publisher,
		metadata.Genre, nullableInteger(metadata.Players), nullableInteger(metadata.ReleaseYear), now)
	if err != nil {
		return fmt.Errorf("insert game asset metadata revision: %w", err)
	}
	return nil
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
	defer cleanup.Rollback(transaction)
	currentID, metadata, version, err := currentGameAssetMetadata(
		request.Context(), transaction, request.PathValue("gameId"),
	)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	revisionID, _ := uuid.NewV7()
	now := server.now().UnixMilli()
	if err := insertGameAssetMetadataRevision(
		request.Context(), transaction, revisionID.String(), request.PathValue("gameId"), metadata, now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := copyAssetsExcept(
		request,
		transaction,
		request.PathValue("gameId"),
		currentID,
		revisionID.String(),
		body.Kind,
		body.Ordinal,
		now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	assetID, consumptionID := newUUIDString(), newUUIDString()
	if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
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
?,
?)
`,
		assetID,
		request.PathValue("gameId"),
		revisionID.String(),
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
	if _, err := transaction.ExecContext(request.Context(), `
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
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET current_metadata_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		revisionID.String(),
		now,
		request.PathValue("gameId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := server.retireSupersededGameAssets(
		request.Context(), transaction, request.PathValue("gameId"), revisionID.String(),
	); err != nil {
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
		writer, request, asset, assetID, revisionID.String(), body.Kind, body.Ordinal, expected+1, now,
	)
}

func writeCreatedGameAsset(
	writer http.ResponseWriter,
	request *http.Request,
	asset preparedGameAsset,
	assetID, revisionID, kind string,
	ordinal int64,
	version, createdAtMS int64,
) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(
		writer,
		http.StatusCreated,
		map[string]any{
			"assetId":            assetID,
			"gameId":             request.PathValue("gameId"),
			"metadataRevisionId": revisionID,
			"kind":               kind,
			"ordinal":            ordinal,
			"widthPx":            asset.width,
			"heightPx":           asset.height,
			"mediaType":          asset.mediaType,
			"version":            version,
			"createdAtMs":        createdAtMS,
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
	defer cleanup.Rollback(transaction)
	currentID, metadata, version, err := currentGameAssetMetadata(
		request.Context(), transaction, request.PathValue("gameId"),
	)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if !gameAssetExists(request.Context(), transaction, request.PathValue("gameId"), currentID, kind) {
		writeError(writer, request, http.StatusNotFound, "ASSET_NOT_FOUND", "媒体不存在", map[string]any{})
		return
	}
	revisionID := newUUIDString()
	now := server.now().UnixMilli()
	if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_metadata_revisions(
id,game_id,title,description,developer,publisher,genre,players,release_year,
source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'ADMIN_EDIT',NULL,?)`, revisionID, request.PathValue("gameId"), metadata.Title,
		metadata.Description, metadata.Developer, metadata.Publisher, metadata.Genre, nullableInteger(metadata.Players),
		nullableInteger(metadata.ReleaseYear), now); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := copyAssetsExcept(
		request, transaction, request.PathValue("gameId"), currentID, revisionID, kind, 0, now,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	result, err := transaction.ExecContext(
		request.Context(),
		`UPDATE games SET current_metadata_revision_id=?,version=version+1,updated_at_ms=? WHERE id=? AND version=?`,
		revisionID,
		now,
		request.PathValue("gameId"),
		expected,
	)
	if err != nil || rowsAffectedHTTP(result) != 1 {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if err := server.retireSupersededGameAssets(
		request.Context(), transaction, request.PathValue("gameId"), revisionID,
	); err != nil {
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

func currentGameAssetMetadata(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
) (string, gameMetadata, int64, error) {
	var currentID string
	var metadata gameMetadata
	var version int64
	err := transaction.QueryRowContext(ctx, `
SELECT g.current_metadata_revision_id,
g.version,
m.title,
m.description,
m.developer,
m.publisher,
m.genre,
m.players,
m.release_year
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
WHERE g.id=?
AND g.status='PUBLISHED'`, gameID).Scan(
		&currentID, &version, &metadata.Title, &metadata.Description, &metadata.Developer, &metadata.Publisher,
		&metadata.Genre, &metadata.Players, &metadata.ReleaseYear,
	)
	if err != nil {
		return "", gameMetadata{}, 0, fmt.Errorf("httpapi/load current game asset metadata: %w", err)
	}
	return currentID, metadata, version, nil
}

func gameAssetExists(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, metadataID, kind string,
) bool {
	var exists int
	return transaction.QueryRowContext(ctx, `
SELECT 1
FROM game_assets
WHERE game_id=?
AND metadata_revision_id=?
AND kind=?
AND ordinal=0`, gameID, metadataID, kind).Scan(&exists) == nil
}

func rowsAffectedHTTP(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	value, _ := result.RowsAffected()
	return value
}

var (
	errReviewAssetUploadInvalid  = errors.New("review asset upload invalid")
	errReviewAssetCASUnavailable = errors.New("review asset CAS unavailable")
	errReviewAssetImageInvalid   = errors.New("review asset image invalid")
	errReviewAssetVersion        = errors.New("review asset version conflict")
	errReviewAssetConsumed       = errors.New("review asset upload consumed")
)

type reviewAssetSource struct {
	uploadID string
	blobID   string
	image    hasheous.AssetData
}

type reviewAssetRecord struct {
	id          string
	createdAtMS int64
}

func (server *Server) loadReviewAssetSource(ctx context.Context, uploadFileID string) (reviewAssetSource, error) {
	var source reviewAssetSource
	var digest string
	if err := server.database.QueryRowContext(ctx, `
SELECT f.upload_session_id,b.id,b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=? AND f.state='COMPLETE'
`, uploadFileID).Scan(&source.uploadID, &source.blobID, &digest); err != nil {
		return reviewAssetSource{}, errReviewAssetUploadInvalid
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		return reviewAssetSource{}, errReviewAssetCASUnavailable
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	cleanup.Error("close", file.Close())
	if readErr != nil {
		return reviewAssetSource{}, errReviewAssetCASUnavailable
	}
	source.image, err = hasheous.ValidateImage(contents, "")
	if err != nil {
		return reviewAssetSource{}, errReviewAssetImageInvalid
	}
	return source, nil
}

func (server *Server) persistReviewAsset(
	ctx context.Context,
	itemID, uploadFileID, kind string,
	expected int64,
	source reviewAssetSource,
) (reviewAssetRecord, error) {
	transaction, err := server.database.BeginTx(ctx, nil)
	if err != nil {
		return reviewAssetRecord{}, fmt.Errorf("begin review asset transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT d.version
FROM review_drafts d
JOIN import_items i ON i.id=d.import_item_id
WHERE i.id=? AND i.state='REVIEW_PENDING'
`, itemID).Scan(&currentVersion); err != nil || currentVersion != expected {
		return reviewAssetRecord{}, errReviewAssetVersion
	}
	var record reviewAssetRecord
	var ownerItemID string
	err = transaction.QueryRowContext(ctx, `
SELECT id,import_item_id,created_at_ms
FROM review_uploaded_assets
WHERE upload_file_id=?
`, uploadFileID).Scan(&record.id, &ownerItemID, &record.createdAtMS)
	switch {
	case err == nil:
		if ownerItemID != itemID {
			return reviewAssetRecord{}, errReviewAssetConsumed
		}
	case errors.Is(err, sql.ErrNoRows):
		record.id = newUUIDString()
		record.createdAtMS = server.now().UnixMilli()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_uploaded_assets(
id,import_item_id,upload_file_id,blob_id,kind,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, record.id, itemID, uploadFileID, source.blobID, kind, source.image.Width, source.image.Height,
			source.image.MediaType, record.createdAtMS); err != nil {
			return reviewAssetRecord{}, fmt.Errorf("insert review uploaded asset: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(
id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms
) VALUES(?,?,?,'REVIEW_ASSET',?,?)
`, newUUIDString(), source.uploadID, uploadFileID, record.id, record.createdAtMS); err != nil {
			return reviewAssetRecord{}, errReviewAssetConsumed
		}
	default:
		return reviewAssetRecord{}, fmt.Errorf("query existing review uploaded asset: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return reviewAssetRecord{}, fmt.Errorf("commit review asset transaction: %w", err)
	}
	return record, nil
}

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
	source, err := server.loadReviewAssetSource(request.Context(), body.UploadFileID)
	switch {
	case errors.Is(err, errReviewAssetUploadInvalid):
		writeError(writer, request, http.StatusUnprocessableEntity, "ASSET_UPLOAD_INVALID", "上传文件不可用", map[string]any{})
		return
	case errors.Is(err, errReviewAssetCASUnavailable):
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "媒体字节不可用", map[string]any{})
		return
	case errors.Is(err, errReviewAssetImageInvalid):
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"ASSET_IMAGE_INVALID",
			"封面必须是受限 PNG、JPEG 或 WebP",
			map[string]any{},
		)
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	record, err := server.persistReviewAsset(
		request.Context(),
		request.PathValue("importItemId"),
		body.UploadFileID,
		body.Kind,
		expected,
		source,
	)
	switch {
	case errors.Is(err, errReviewAssetVersion):
		writeError(writer, request, http.StatusConflict, "REVIEW_VERSION_CONFLICT", "审核条目已发生变化", map[string]any{})
		return
	case errors.Is(err, errReviewAssetConsumed):
		writeError(writer, request, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "上传文件已被其他操作占用", map[string]any{})
		return
	case err != nil:
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected))
	writeJSON(writer, http.StatusCreated, map[string]any{
		"assetId": record.id, "kind": body.Kind, "widthPx": source.image.Width, "heightPx": source.image.Height,
		"mediaType": source.image.MediaType, "url": "/api/v1/admin/review-assets/" + record.id,
		"version": expected, "createdAtMs": record.createdAtMS,
	})
}

func copyAssetsExcept(
	request *http.Request,
	transaction *sql.Tx,
	gameID, currentID, revisionID, skipKind string,
	skipOrdinal int64,
	now int64,
) error {
	rows, err := transaction.QueryContext(
		request.Context(),
		`
SELECT blob_id,
kind,
ordinal,
width_px,
height_px,
media_type
FROM game_assets
WHERE game_id=?
AND metadata_revision_id=?
ORDER BY kind,
ordinal
`,
		gameID,
		currentID,
	)
	if err != nil {
		return fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var blobID, kind, mediaType string
		var ordinal int64
		var width, height sql.NullInt64
		if err := rows.Scan(&blobID, &kind, &ordinal, &width, &height, &mediaType); err != nil {
			return fmt.Errorf("httpapi/game_handlers: %w", err)
		}
		if kind == skipKind && ordinal == skipOrdinal {
			continue
		}
		if _, err := transaction.ExecContext(request.Context(), `
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
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
?,
?)
	`, newUUIDString(), gameID, revisionID, blobID, kind, ordinal,
			nullableInteger(width), nullableInteger(height), mediaType, now); err != nil {
			return fmt.Errorf("httpapi/game_handlers: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan game media: %w", err)
	}
	return nil
}

func newUUIDString() string {
	value, _ := uuid.NewV7()
	return value.String()
}

func (server *Server) contentAsset(writer http.ResponseWriter, request *http.Request) {
	var digest, mediaType string
	err := server.database.QueryRowContext(request.Context(), `
SELECT b.sha256,
a.media_type
FROM game_assets a
JOIN blobs b ON b.id=a.blob_id
JOIN games g ON g.id=a.game_id
WHERE a.id=?
AND g.status='PUBLISHED'
AND a.metadata_revision_id=g.current_metadata_revision_id
`, request.PathValue("assetId")).
		Scan(&digest, &mediaType)
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "ASSET_NOT_FOUND", "媒体不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest, mediaType, false)
}

func (server *Server) saveStateScreenshot(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	var digest, mediaType string
	err := server.database.QueryRowContext(request.Context(), `
SELECT b.sha256,
b.media_type
FROM save_states s
JOIN blobs b ON b.id=s.screenshot_blob_id
JOIN games g ON g.id=s.game_id
WHERE s.id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
AND g.status='PUBLISHED'
`, request.PathValue("saveStateId"), principal.ProfileID).Scan(&digest, &mediaType)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusNotFound,
			"SAVE_SCREENSHOT_NOT_FOUND",
			"存档截图不存在",
			map[string]any{},
		)
		return
	}
	server.serveBlob(writer, request, digest, mediaType, true)
}
