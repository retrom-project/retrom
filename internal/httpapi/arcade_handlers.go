package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"retrom/internal/arcadecatalog"
	"retrom/internal/cursor"
)

func (server *Server) createArcadeDAT(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body arcadecatalog.CreateRequest
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "DAT 候选请求无效", map[string]any{})
		return
	}
	created, err := server.arcadeDAT.Create(request.Context(), body)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"DAT_INPUT_INVALID",
			"上传文件或 CoreArtifact 不可用于 DAT 候选",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusAccepted, created)
}

func (server *Server) arcadeDATDiff(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	section := values.Get("section")
	if section == "" {
		section = "MACHINES"
	}
	change := values.Get("change")
	if change == "" {
		change = "ALL"
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 100 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "DAT 差异分页参数无效", map[string]any{})
			return
		}
		limit = parsed
	}
	filterDigest := cursor.FilterDigest(
		map[string]any{"datVersionId": request.PathValue("datVersionId"), "section": section, "change": change},
	)
	after := ""
	if token := values.Get("cursor"); token != "" {
		payload, decodeErr := server.cursors.Decode(token, "getAdminArcadeDATDiff", filterDigest, "DAT_DIFF_ASC")
		if decodeErr != nil || len(payload.SortValues) != 0 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "DAT 差异游标无效", map[string]any{})
			return
		}
		after = payload.ID
	}
	diff, err := server.arcadeDAT.Diff(
		request.Context(),
		request.PathValue("datVersionId"),
		arcadecatalog.DiffOptions{Section: section, Change: change, After: after, Limit: limit},
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "DAT_NOT_READY", "DAT 尚未完成安全解析或差异参数无效", map[string]any{})
		return
	}
	if diff.HasMore {
		token, encodeErr := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminArcadeDATDiff",
				FilterDigest: filterDigest,
				SortCode:     "DAT_DIFF_ASC",
				ID:           diff.LastCursorKey,
			},
		)
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		diff.NextCursor = token
	}
	writeJSON(writer, http.StatusOK, diff)
}

func (server *Server) activateArcadeDAT(writer http.ResponseWriter, request *http.Request) {
	server.changeArcadeDAT(writer, request, false)
}

func (server *Server) rollbackArcadeDAT(writer http.ResponseWriter, request *http.Request) {
	server.changeArcadeDAT(writer, request, true)
}

func (server *Server) changeArcadeDAT(writer http.ResponseWriter, request *http.Request, rollback bool) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body arcadecatalog.ActivateRequest
	if decodeJSON(writer, request, &body, 16<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "DAT 启用请求无效", map[string]any{})
		return
	}
	result, err := server.arcadeDAT.Activate(
		request.Context(),
		request.PathValue("datVersionId"),
		version,
		body,
		rollback,
	)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"IMPACT_PREVIEW_STALE",
			"DAT 差异、兼容确认或 CoreArtifact 版本已经变化",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) deleteArcadeDAT(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if err := server.arcadeDAT.Delete(request.Context(), request.PathValue("datVersionId"), version); err != nil {
		writeError(writer, request, http.StatusConflict, "DAT_DELETE_CONFLICT", "DAT 已启用、被引用或版本已经变化", map[string]any{})
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
