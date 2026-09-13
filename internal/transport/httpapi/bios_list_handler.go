package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/foundation/cursor"
	biosservice "retrom/internal/service/bios"
)

type biosQuery struct {
	scope, quick, status string
	limit                int
}

func (server *Server) bios(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	parsed, message := parseBIOSQuery(values)
	if message != "" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", message, map[string]any{})
		return
	}
	filterDigest := cursor.FilterDigest(
		map[string]any{
			"scope":      parsed.scope,
			"q":          strings.TrimSpace(values.Get("q")),
			"platformId": values.Get("platformId"),
			"coreId":     values.Get("coreId"),
			"providerId": values.Get("providerId"),
			"targetId":   values.Get("targetId"),
			"status":     parsed.status,
			"quick":      parsed.quick,
		},
	)
	var pageCursor *biosservice.Cursor
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminBIOS", filterDigest, "BIOS_CATALOG_ASC")
		if err != nil || len(payload.SortValues) != 2 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		pageCursor = &biosservice.Cursor{SortValues: payload.SortValues, ID: payload.ID}
	}
	result, err := server.biosService.List(request.Context(), biosservice.ListRequest{
		Scope:      parsed.scope,
		Query:      strings.TrimSpace(values.Get("q")),
		PlatformID: values.Get("platformId"),
		CoreID:     values.Get("coreId"),
		ProviderID: values.Get("providerId"),
		TargetID:   values.Get("targetId"),
		Status:     parsed.status,
		Quick:      parsed.quick,
		Limit:      parsed.limit,
		Cursor:     pageCursor,
	})
	if errors.Is(err, biosservice.ErrInvalid) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "BIOS 查询参数无效", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, projectBIOSItem(item))
	}
	var next *string
	if result.NextCursor != nil {
		token, encodeErr := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminBIOS",
				FilterDigest: filterDigest,
				SortCode:     "BIOS_CATALOG_ASC",
				SortValues:   result.NextCursor.SortValues,
				ID:           result.NextCursor.ID,
			},
		)
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		next = &token
	}
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"generatedAtMs": server.now().UnixMilli(),
			"scope":         parsed.scope,
			"scopeCounts": map[string]int64{
				"requiredByLibrary": result.ScopeCounts.RequiredByLibrary,
				"fullCatalog":       result.ScopeCounts.FullCatalog,
			},
			"summary": map[string]int64{
				"totalCount": result.Summary.TotalCount, "blockingCount": result.Summary.BlockingCount,
				"warningCount": result.Summary.WarningCount, "readyCount": result.Summary.ReadyCount,
				"attentionCount": result.Summary.AttentionCount, "requiredCount": result.Summary.RequiredCount,
				"optionalCount": result.Summary.OptionalCount,
			},
			"filteredCount": result.FilteredCount,
			"items":         items,
			"nextCursor":    next,
		},
	)
}

func projectBIOSItem(item biosservice.Item) map[string]any {
	var installation any
	if item.ActiveInstallation != nil {
		installation = map[string]any{
			"id": item.ActiveInstallation.ID, "md5": item.ActiveInstallation.MD5,
			"sha1": item.ActiveInstallation.SHA1, "sha256": item.ActiveInstallation.SHA256,
			"validatedRequirementVersion": item.ActiveInstallation.ValidatedRequirementVersion,
			"createdAtMs":                 item.ActiveInstallation.CreatedAtMS,
		}
	}
	return map[string]any{
		"id": item.ID, "coreId": item.CoreID, "coreName": item.CoreName,
		"providerId": item.ProviderID, "targetId": item.TargetID,
		"logicalName": item.LogicalName, "sourceKind": item.SourceKind,
		"fileKind": item.FileKind, "requirementMode": item.RequirementMode,
		"conditionCode": item.ConditionCode, "expectedMd5": item.ExpectedMD5,
		"enabled": item.Enabled, "version": item.Version, "status": item.Status,
		"activeInstallation": installation,
	}
}

func parseBIOSQuery(values url.Values) (biosQuery, string) {
	result := biosQuery{scope: values.Get("scope"), quick: values.Get("quick"), status: values.Get("status"), limit: 100}
	if result.scope == "" {
		result.scope = "REQUIRED_BY_LIBRARY"
	}
	if result.scope != "FULL_CATALOG" && result.scope != "REQUIRED_BY_LIBRARY" {
		return biosQuery{}, "BIOS 需求范围无效"
	}
	if result.quick == "" {
		result.quick = "ALL"
	}
	if result.quick != "ALL" && result.quick != "ATTENTION" &&
		result.quick != "REQUIRED" && result.quick != "OPTIONAL" {
		return biosQuery{}, "BIOS 快速筛选无效"
	}
	if !validBIOSStatus(result.status) {
		return biosQuery{}, "BIOS 状态无效"
	}
	if values.Get("limit") != "" {
		result.limit, _ = strconv.Atoi(values.Get("limit"))
	}
	return result, ""
}

func validBIOSStatus(status string) bool {
	switch status {
	case "", "MATCHED", "MISSING", "HASH_WARNING", "MISSING_ENTRY", "OPTIONAL_MISSING", "INVALID":
		return true
	default:
		return false
	}
}
