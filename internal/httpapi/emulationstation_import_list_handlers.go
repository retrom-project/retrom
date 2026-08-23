package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/cursor"
)

func (server *Server) emulationStationImportList(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	state := values.Get("state")
	if state != "" && !validEmulationStationState(state) {
		writeError(
			writer, request, http.StatusBadRequest, "INVALID_QUERY",
			"EmulationStation 导入状态无效", map[string]any{},
		)
		return
	}
	limit := 20
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"state": state})
	var beforeAt int64
	beforeID := ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(
			token,
			"getAdminEmulationStationImports",
			filter,
			"EMULATIONSTATION_IMPORT_CREATED_DESC",
		)
		if err != nil || len(payload.SortValues) != 1 {
			writeInvalidEmulationStationCursor(writer, request)
			return
		}
		beforeAt, err = strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			writeInvalidEmulationStationCursor(writer, request)
			return
		}
		beforeID = payload.ID
	}
	items, err := server.emulationStationImports.List(
		request.Context(), state, beforeAt, beforeID, limit+1,
	)
	if err != nil {
		server.writeEmulationStationImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(cursor.Payload{
			OperationID:  "getAdminEmulationStationImports",
			FilterDigest: filter,
			SortCode:     "EMULATIONSTATION_IMPORT_CREATED_DESC",
			SortValues:   []string{strconv.FormatInt(last.CreatedAtMS, 10)},
			ID:           last.ID,
		})
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) emulationStationImportGamelists(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("emulationStationImportId")
	values := request.URL.Query()
	parseState := values.Get("parseState")
	if parseState != "" && parseState != "VALID" && parseState != "INVALID" {
		writeError(
			writer, request, http.StatusBadRequest, "INVALID_QUERY",
			"gamelist 解析状态无效", map[string]any{},
		)
		return
	}
	limit := 100
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"id": importID, "parseState": parseState})
	afterPath := ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(
			token,
			"getAdminEmulationStationImportGamelists",
			filter,
			"EMULATIONSTATION_GAMELIST_ASC",
		)
		if err != nil || len(payload.SortValues) != 1 {
			writeInvalidEmulationStationCursor(writer, request)
			return
		}
		afterPath = payload.SortValues[0]
	}
	items, err := server.emulationStationImports.Gamelists(
		request.Context(), importID, parseState, afterPath, limit+1,
	)
	if err != nil {
		server.writeEmulationStationImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(cursor.Payload{
			OperationID:  "getAdminEmulationStationImportGamelists",
			FilterDigest: filter,
			SortCode:     "EMULATIONSTATION_GAMELIST_ASC",
			SortValues:   []string{last.RelativePath},
			ID:           last.RelativePath,
		})
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) emulationStationImportCollections(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("emulationStationImportId")
	limit := 100
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"id": importID})
	afterPath, afterID := "", ""
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(
			token,
			"getAdminEmulationStationImportCollections",
			filter,
			"EMULATIONSTATION_COLLECTION_ASC",
		)
		if err != nil || len(payload.SortValues) != 1 {
			writeInvalidEmulationStationCursor(writer, request)
			return
		}
		afterPath, afterID = payload.SortValues[0], payload.ID
	}
	items, err := server.emulationStationImports.Collections(
		request.Context(), importID, afterPath, afterID, limit+1,
	)
	if err != nil {
		server.writeEmulationStationImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(cursor.Payload{
			OperationID:  "getAdminEmulationStationImportCollections",
			FilterDigest: filter,
			SortCode:     "EMULATIONSTATION_COLLECTION_ASC",
			SortValues:   []string{last.GamelistRelativePath},
			ID:           last.ID,
		})
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) emulationStationImportItems(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("emulationStationImportId")
	values := request.URL.Query()
	limit := 50
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	query := strings.TrimSpace(values.Get("q"))
	filters := map[string]any{
		"id": importID, "q": query, "outcome": values.Get("outcome"),
		"warning": values.Get("warning"), "collectionId": values.Get("collectionId"),
	}
	filter := cursor.FilterDigest(filters)
	afterTitle, afterID := "", ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(
			token,
			"getAdminEmulationStationImportItems",
			filter,
			"EMULATIONSTATION_ITEM_ASC",
		)
		if err != nil || len(payload.SortValues) != 1 {
			writeInvalidEmulationStationCursor(writer, request)
			return
		}
		afterTitle, afterID = payload.SortValues[0], payload.ID
	}
	items, err := server.emulationStationImports.Items(
		request.Context(), importID, query, values.Get("outcome"), values.Get("warning"),
		values.Get("collectionId"), afterTitle, afterID, limit+1,
	)
	if err != nil {
		server.writeEmulationStationImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(cursor.Payload{
			OperationID:  "getAdminEmulationStationImportItems",
			FilterDigest: filter,
			SortCode:     "EMULATIONSTATION_ITEM_ASC",
			SortValues:   []string{last.Title},
			ID:           last.ID,
		})
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func writeInvalidEmulationStationCursor(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
}

func validEmulationStationState(value string) bool {
	switch value {
	case "SCANNING",
		"AWAITING_MAPPING",
		"QUEUED",
		"RUNNING",
		"PARTIAL_FAILURE",
		"COMPLETED",
		"CANCEL_REQUESTED",
		"CANCELLED",
		"FAILED",
		"EXPIRED":
		return true
	}
	return false
}
