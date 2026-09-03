package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/cursor"
)

const biosStatusExpression = "COALESCE(installation.status," +
	"CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 'OPTIONAL_MISSING' ELSE 'MISSING' END)"

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
	scopeSQL := biosScopeSQL(parsed.scope)
	conditions, arguments := biosFilters(values, scopeSQL, parsed.status, parsed.quick)
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
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminBIOS", filterDigest, "BIOS_CATALOG_ASC")
		if err != nil || len(payload.SortValues) != 2 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		conditions = append(
			conditions,
			"(core.name>? OR (core.name=? AND requirement.logical_name>?) OR "+
				"(core.name=? AND requirement.logical_name=? AND requirement.id>?))",
		)
		arguments = append(
			arguments,
			payload.SortValues[0],
			payload.SortValues[0],
			payload.SortValues[1],
			payload.SortValues[0],
			payload.SortValues[1],
			payload.ID,
		)
	}
	scopeCounts, summary, filtered, err := server.biosAggregates(request, scopeSQL)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	query := `
SELECT requirement.id,requirement.core_id,core.name,requirement.provider_id,requirement.target_id,
requirement.target_contract_sha256,requirement.logical_name,
requirement.source_kind,requirement.requirement_mode,requirement.condition_code,requirement.md5,requirement.enabled,
requirement.version,` + biosStatusExpression + `,installation.id,installation.md5,installation.sha1,installation.sha256,
installation.validated_requirement_version,installation.created_at_ms
FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id AND installation.is_active=1
WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY core.name COLLATE BINARY,
requirement.logical_name COLLATE BINARY,requirement.id COLLATE BINARY LIMIT ?`
	pageArguments := append(append([]any{}, arguments...), parsed.limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, pageArguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0, parsed.limit+1)
	sortNames := make([][3]string, 0, parsed.limit+1)
	for rows.Next() {
		var id, coreID, coreName, providerID, targetID, targetContract string
		var logicalName, sourceKind, mode, itemStatus string
		var condition, expectedMD5, installationID, installedMD5, installedSHA1, installedSHA256 sql.NullString
		var validatedVersion, installedAt sql.NullInt64
		var enabled int
		var version int64
		if err := rows.Scan(
			&id, &coreID, &coreName, &providerID, &targetID, &targetContract,
			&logicalName, &sourceKind, &mode, &condition,
			&expectedMD5, &enabled, &version, &itemStatus, &installationID, &installedMD5,
			&installedSHA1, &installedSHA256, &validatedVersion, &installedAt,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"id":                   id,
				"coreId":               coreID,
				"coreName":             coreName,
				"providerId":           providerID,
				"targetId":             targetID,
				"targetContractSha256": targetContract,
				"logicalName":          logicalName,
				"sourceKind":           sourceKind,
				"requirementMode":      mode,
				"conditionCode":        nullableString(condition),
				"expectedMd5":          nullableString(expectedMD5),
				"enabled":              enabled == 1,
				"version":              version,
				"status":               itemStatus,
				"activeInstallation": nullableBIOSInstallation(
					installationID,
					installedMD5,
					installedSHA1,
					installedSHA256,
					validatedVersion,
					installedAt,
				),
			},
		)
		sortNames = append(sortNames, [3]string{coreName, logicalName, id})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var next *string
	if len(items) > parsed.limit {
		items = items[:parsed.limit]
		sortNames = sortNames[:parsed.limit]
		last := sortNames[len(sortNames)-1]
		token, encodeErr := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminBIOS",
				FilterDigest: filterDigest,
				SortCode:     "BIOS_CATALOG_ASC",
				SortValues:   []string{last[0], last[1]},
				ID:           last[2],
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
			"scopeCounts":   scopeCounts,
			"summary":       summary,
			"filteredCount": filtered,
			"items":         items,
			"nextCursor":    next,
		},
	)
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

func biosFilters(values url.Values, scopeSQL, status, quick string) ([]string, []any) {
	conditions := []string{"requirement.enabled=1", scopeSQL}
	arguments := make([]any, 0, 12)
	if value := strings.TrimSpace(values.Get("q")); value != "" {
		conditions = append(conditions,
			"(instr(lower(requirement.logical_name),lower(?))>0 OR instr(lower(core.name),lower(?))>0)")
		arguments = append(arguments, value, value)
	}
	for _, filter := range []struct{ name, column string }{
		{"coreId", "requirement.core_id"},
		{"providerId", "requirement.provider_id"},
		{"targetId", "requirement.target_id"},
	} {
		if value := values.Get(filter.name); value != "" {
			conditions = append(conditions, filter.column+"=?")
			arguments = append(arguments, value)
		}
	}
	if value := values.Get("platformId"); value != "" {
		conditions = append(conditions, `EXISTS(SELECT 1 FROM platform_cores platform_core
WHERE platform_core.core_id=requirement.core_id AND platform_core.platform_id=?)`)
		arguments = append(arguments, value)
	}
	if status != "" {
		conditions = append(conditions, biosStatusExpression+"=?")
		arguments = append(arguments, status)
	}
	return appendBIOSQuickFilter(conditions, quick), arguments
}

func appendBIOSQuickFilter(conditions []string, quick string) []string {
	switch quick {
	case "ATTENTION":
		return append(conditions, "((requirement.requirement_mode<>'OPTIONAL' AND "+
			biosStatusExpression+" IN ('MISSING','MISSING_ENTRY','INVALID')) OR "+
			biosStatusExpression+"='HASH_WARNING')")
	case "REQUIRED":
		return append(conditions, "requirement.requirement_mode='REQUIRED'")
	case "OPTIONAL":
		return append(conditions, "requirement.requirement_mode='OPTIONAL'")
	default:
		return conditions
	}
}

// The EXISTS clause is a fixed library-scope predicate shared by list and aggregate queries.
func biosScopeSQL(scope string) string {
	if scope == "FULL_CATALOG" {
		return "1=1"
	}
	return `EXISTS(SELECT 1 FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN games game ON game.id=variant.game_id
WHERE revision.provider_id=requirement.provider_id AND revision.target_id=requirement.target_id
 AND game.status='PUBLISHED')`
}

// Aggregate SQL expressions remain aligned with the status projection above.
func (server *Server) biosAggregates(
	request *http.Request,
	scopeSQL string,
) (map[string]int64, map[string]int64, int64, error) {
	var requiredByLibrary, fullCatalog int64
	if err := server.database.QueryRowContext(request.Context(), `
SELECT COALESCE(sum(CASE WHEN `+biosScopeSQL("REQUIRED_BY_LIBRARY")+` THEN 1 ELSE 0 END),0),count(*)
FROM bios_requirements requirement WHERE requirement.enabled=1
`).Scan(&requiredByLibrary, &fullCatalog); err != nil {
		return nil, nil, 0, fmt.Errorf("aggregate BIOS scopes: %w", err)
	}
	counts := map[string]int64{"requiredByLibrary": requiredByLibrary, "fullCatalog": fullCatalog}
	var total, blocking, warning, ready, attention, required, optional int64
	if err := server.database.QueryRowContext(request.Context(), `
SELECT count(*),
COALESCE(sum(CASE WHEN requirement.requirement_mode<>'OPTIONAL' AND `+biosStatusExpression+`
 IN ('MISSING','MISSING_ENTRY','INVALID') THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN `+biosStatusExpression+`='HASH_WARNING' THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN `+biosStatusExpression+`='MATCHED' THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN (requirement.requirement_mode<>'OPTIONAL' AND `+biosStatusExpression+`
 IN ('MISSING','MISSING_ENTRY','INVALID')) OR `+biosStatusExpression+`='HASH_WARNING' THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN requirement.requirement_mode='REQUIRED' THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 1 ELSE 0 END),0)
FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
 AND installation.is_active=1 WHERE requirement.enabled=1 AND `+scopeSQL).Scan(
		&total, &blocking, &warning, &ready, &attention, &required, &optional,
	); err != nil {
		return nil, nil, 0, fmt.Errorf("aggregate BIOS status: %w", err)
	}
	summary := map[string]int64{
		"totalCount":     total,
		"blockingCount":  blocking,
		"warningCount":   warning,
		"readyCount":     ready,
		"attentionCount": attention,
		"requiredCount":  required,
		"optionalCount":  optional,
	}
	// Rebuild only the non-cursor arguments for the filtered aggregate.
	values := request.URL.Query()
	conditions, args := biosFilters(values, scopeSQL, values.Get("status"), values.Get("quick"))
	var filtered int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT count(*) FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
AND installation.is_active=1 WHERE `+strings.Join(conditions, " AND "), args...).
		Scan(&filtered)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, 0, fmt.Errorf("aggregate filtered BIOS catalog: %w", err)
	}
	return counts, summary, filtered, nil
}
