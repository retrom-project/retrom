package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/gametitle"
	"retrom/internal/libraryimport"
	"retrom/internal/mediaasset"
)

type applyCandidateRequest struct {
	Fields         []string                     `json:"fields"`
	SelectedAssets libraryimport.SelectedAssets `json:"selectedAssets"`
}

// Candidate freshness, metadata changes, selected media replacement, and optimistic locking share one transaction.
func (server *Server) applyGameScrapeCandidate(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body applyCandidateRequest
	if decodeJSON(writer, request, &body, 32<<10) != nil || !validCandidateFields(body.Fields) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "候选采用字段无效", map[string]any{})
		return
	}
	transaction, err := server.database.BeginTx(request.Context(), nil)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	var candidateMetadataJSON string
	var current gameMetadata
	var version int64
	err = transaction.QueryRowContext(request.Context(), `
SELECT g.version,
g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year,
c.normalized_metadata_json
FROM games g
JOIN scrape_candidates c ON c.id=?
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
AND r.game_id=g.id
AND r.state='COMPLETED'
WHERE g.id=?
AND g.status='PUBLISHED'
AND r.id=(SELECT id
FROM metadata_scrape_runs
WHERE game_id=g.id
AND provider='HASHEOUS'
AND state='COMPLETED'
ORDER BY created_at_ms DESC,
id DESC LIMIT 1)
`, request.PathValue("candidateId"), request.PathValue("gameId")).
		Scan(
			&version,
			&current.Title,
			&current.Description,
			&current.Developer,
			&current.Publisher,
			&current.Genre,
			&current.Players,
			&current.ReleaseYear,
			&candidateMetadataJSON,
		)
	if err != nil || version != expected {
		writeError(writer, request, http.StatusConflict, "SCRAPE_CANDIDATE_STALE", "候选不是当前内容的最新批次", map[string]any{})
		return
	}
	var candidate map[string]any
	if json.Unmarshal([]byte(candidateMetadataJSON), &candidate) != nil {
		server.databaseError(writer, request, errCandidateMetadata)
		return
	}
	applyMetadataFields(&current, candidate, body.Fields)
	if strings.TrimSpace(current.Title) == "" {
		writeError(writer, request, http.StatusUnprocessableEntity, "SCRAPE_METADATA_INVALID", "候选元数据无效", map[string]any{})
		return
	}
	assetIDs, replacedBlobIDs, err := replaceCandidateAssets(
		request.Context(), transaction, request.PathValue("gameId"), request.PathValue("candidateId"),
		body.SelectedAssets, server.now().UnixMilli(),
	)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"SCRAPE_ASSET_INVALID",
			"候选媒体不可用或归属不匹配",
			map[string]any{},
		)
		return
	}
	now := server.now().UnixMilli()
	result, err := transaction.ExecContext(
		request.Context(),
		`
UPDATE games
SET title=?,
title_initial=?,
description=?,
developer=?,
publisher=?,
genre=?,
players=?,
release_year=?,
metadata_source_kind='RESCRAPE_APPLY',
metadata_source_ref_id=?,
search_text=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
`,
		current.Title,
		gametitle.Initial(current.Title),
		current.Description,
		current.Developer,
		current.Publisher,
		current.Genre,
		nullableInteger(current.Players),
		nullableInteger(current.ReleaseYear),
		request.PathValue("candidateId"),
		strings.ToLower(
			strings.Join([]string{current.Title, current.Developer, current.Publisher, current.Genre}, " "),
		),
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
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId": request.PathValue("gameId"), "assetIds": assetIDs,
			"version": expected + 1, "updatedAtMs": now,
		},
	)
}

func validCandidateFields(fields []string) bool {
	allowed := map[string]struct{}{
		"title":       {},
		"description": {},
		"developer":   {},
		"publisher":   {},
		"genre":       {},
		"players":     {},
		"releaseYear": {},
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := allowed[field]; !ok {
			return false
		}
		if _, duplicate := seen[field]; duplicate {
			return false
		}
		seen[field] = struct{}{}
	}
	return true
}

func applyMetadataFields(target *gameMetadata, candidate map[string]any, fields []string) {
	for _, field := range fields {
		switch field {
		case "title":
			target.Title, _ = candidate[field].(string)
		case "description":
			target.Description, _ = candidate[field].(string)
		case "developer":
			target.Developer, _ = candidate[field].(string)
		case "publisher":
			target.Publisher, _ = candidate[field].(string)
		case "genre":
			target.Genre, _ = candidate[field].(string)
		case "players":
			if candidate[field] == nil {
				target.Players = sql.NullInt64{}
			} else if value, ok := candidate[field].(float64); ok {
				target.Players = sql.NullInt64{Int64: int64(value), Valid: true}
			}
		case "releaseYear":
			if candidate[field] == nil {
				target.ReleaseYear = sql.NullInt64{}
			} else if value, ok := candidate[field].(float64); ok {
				target.ReleaseYear = sql.NullInt64{Int64: int64(value), Valid: true}
			}
		}
	}
}

type selectedCandidateAsset struct {
	id      string
	kind    string
	ordinal int64
}

func replaceCandidateAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, candidateID string,
	selected libraryimport.SelectedAssets,
	now int64,
) ([]string, []string, error) {
	selectedAssets := selectedCandidateAssets(selected)
	for _, selectedAsset := range selectedAssets {
		if err := validateSelectedCandidateAsset(ctx, transaction, candidateID, selectedAsset); err != nil {
			return nil, nil, err
		}
	}
	replacedBlobIDs := make([]string, 0)
	for _, kind := range selectedAssetKinds(selected) {
		blobIDs, err := removeGameAssetsByKind(ctx, transaction, gameID, kind)
		if err != nil {
			return nil, nil, err
		}
		replacedBlobIDs = append(replacedBlobIDs, blobIDs...)
	}
	createdIDs := make([]string, 0, len(selectedAssets))
	for _, selectedAsset := range selectedAssets {
		createdID, err := createSelectedCandidateAsset(
			ctx, transaction, gameID, candidateID, selectedAsset, now,
		)
		if err != nil {
			return nil, nil, err
		}
		createdIDs = append(createdIDs, createdID)
	}
	return createdIDs, replacedBlobIDs, nil
}

func selectedAssetKinds(selected libraryimport.SelectedAssets) []string {
	kinds := make([]string, 0, 3)
	if selected.CoverCandidateAssetID != nil {
		kinds = append(kinds, "COVER")
	}
	if selected.BackgroundCandidateAssetID != nil {
		kinds = append(kinds, "BACKGROUND")
	}
	if len(selected.ScreenshotCandidateAssetIDs) > 0 {
		kinds = append(kinds, "SCREENSHOT")
	}
	return kinds
}

func removeGameAssetsByKind(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, kind string,
) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT blob_id FROM game_assets WHERE game_id=? AND kind=? ORDER BY ordinal,id
`, gameID, kind)
	if err != nil {
		return nil, fmt.Errorf("httpapi/list replaced candidate assets: %w", err)
	}
	defer func() { cleanup.Error("close replaced candidate assets", rows.Close()) }()
	blobIDs := make([]string, 0)
	for rows.Next() {
		var blobID string
		if err := rows.Scan(&blobID); err != nil {
			return nil, fmt.Errorf("httpapi/scan replaced candidate asset: %w", err)
		}
		blobIDs = append(blobIDs, blobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi/iterate replaced candidate assets: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx, `DELETE FROM game_assets WHERE game_id=? AND kind=?`, gameID, kind,
	); err != nil {
		return nil, fmt.Errorf("httpapi/delete replaced candidate assets: %w", err)
	}
	return blobIDs, nil
}

func selectedCandidateAssets(selected libraryimport.SelectedAssets) []selectedCandidateAsset {
	assets := make([]selectedCandidateAsset, 0, len(selected.ScreenshotCandidateAssetIDs)+2)
	if selected.CoverCandidateAssetID != nil {
		assets = append(
			assets,
			selectedCandidateAsset{id: *selected.CoverCandidateAssetID, kind: "COVER", ordinal: 0},
		)
	}
	if selected.BackgroundCandidateAssetID != nil {
		assets = append(
			assets,
			selectedCandidateAsset{id: *selected.BackgroundCandidateAssetID, kind: "BACKGROUND", ordinal: 0},
		)
	}
	for ordinal, selectedID := range selected.ScreenshotCandidateAssetIDs {
		assets = append(
			assets,
			selectedCandidateAsset{id: selectedID, kind: "SCREENSHOT", ordinal: int64(ordinal)},
		)
	}
	return assets
}

func createSelectedCandidateAsset(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, candidateID string,
	selected selectedCandidateAsset,
	now int64,
) (string, error) {
	var blobID, kind, mediaType string
	var sourceOrdinal, width, height int64
	err := transaction.QueryRowContext(ctx, `
SELECT blob_id,
kind_hint,
ordinal,
width_px,
height_px,
media_type
FROM scrape_candidate_assets
WHERE id=?
AND scrape_candidate_id=?
AND status='READY'
`, selected.id, candidateID).
		Scan(&blobID, &kind, &sourceOrdinal, &width, &height, &mediaType)
	if err != nil {
		return "", fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	if kind != selected.kind {
		return "", errCandidateAssetKind
	}
	assetID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
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
		assetID.String(),
		gameID,
		blobID,
		kind,
		selected.ordinal,
		width,
		height,
		mediaType,
		now,
	); err != nil {
		return "", fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	return assetID.String(), nil
}

func validateSelectedCandidateAsset(
	ctx context.Context,
	transaction *sql.Tx,
	candidateID string,
	selected selectedCandidateAsset,
) error {
	var kind string
	err := transaction.QueryRowContext(ctx, `
SELECT kind_hint FROM scrape_candidate_assets
WHERE id=? AND scrape_candidate_id=? AND status='READY'
`, selected.id, candidateID).Scan(&kind)
	if err != nil {
		return fmt.Errorf("httpapi/game_handlers: %w", err)
	}
	if kind != selected.kind {
		return errCandidateAssetKind
	}
	return nil
}

func inspectUploadedGameAsset(
	file io.ReadSeeker,
	blobSize int64,
	kind string,
) (string, *int64, *int64, string, string) {
	if kind == "VIDEO" {
		mediaType, err := mediaasset.InspectVideo(file, blobSize)
		if err != nil {
			return "", nil, nil, "ASSET_VIDEO_INVALID", "视频必须是受限 MP4 或 WebM"
		}
		return mediaType, nil, nil, "", ""
	}
	imageData, err := mediaasset.InspectImage(file, blobSize)
	if err != nil {
		return "", nil, nil, "ASSET_IMAGE_INVALID", "媒体必须是受限 PNG、JPEG 或 WebP"
	}
	return imageData.MediaType, &imageData.WidthPX, &imageData.HeightPX, "", ""
}
