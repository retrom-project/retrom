package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	gamemetadatamodel "retrom/internal/model/gamemetadata"
	gamemetadata "retrom/internal/service/gamemetadata"
)

type applyCandidateRequest struct {
	Fields         []string                         `json:"fields"`
	SelectedAssets gamemetadatamodel.SelectedAssets `json:"selectedAssets"`
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
	if decodeJSON(writer, request, &body, 32<<10) != nil || !gamemetadata.ValidCandidateFields(body.Fields) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "候选采用字段无效", map[string]any{})
		return
	}
	result, err := server.gameMetadata.ApplyCandidate(
		request.Context(), gamemetadatamodel.ApplyCandidateRequest{
			GameID: request.PathValue("gameId"), CandidateID: request.PathValue("candidateId"),
			ExpectedVersion: expected, Fields: body.Fields, SelectedAssets: body.SelectedAssets,
		},
	)
	if errors.Is(err, gamemetadatamodel.ErrCandidateStale) {
		writeError(writer, request, http.StatusConflict, "SCRAPE_CANDIDATE_STALE", "候选不是当前内容的最新批次", map[string]any{})
		return
	}
	if errors.Is(err, gamemetadatamodel.ErrCandidateMetadata) {
		server.databaseError(writer, request, errCandidateMetadata)
		return
	}
	if errors.Is(err, gamemetadatamodel.ErrMetadataInvalid) {
		writeError(writer, request, http.StatusUnprocessableEntity, "SCRAPE_METADATA_INVALID", "候选元数据无效", map[string]any{})
		return
	}
	if errors.Is(err, gamemetadatamodel.ErrCandidateAsset) {
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
	if errors.Is(err, gamemetadatamodel.ErrVersionConflict) {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "游戏已被修改", map[string]any{})
		return
	}
	if errors.Is(err, gamemetadatamodel.ErrInvalid) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "候选采用字段无效", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if server.payloadReleases != nil {
		server.payloadReleases.Signal()
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"gameId": request.PathValue("gameId"), "assetIds": result.AssetIDs,
			"version": result.Version, "updatedAtMs": result.UpdatedAtMS,
		},
	)
}
