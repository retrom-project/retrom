package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/cursor"
)

//nolint:funlen,gocognit,gocyclo,gosec,lll // SQL fragments come only from closed filters; values remain placeholders.
func (server *Server) bios(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	scope := values.Get("scope")
	if scope == "" {
		scope = "REQUIRED_BY_LIBRARY"
	}
	if scope != "FULL_CATALOG" && scope != "REQUIRED_BY_LIBRARY" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "BIOS 需求范围无效", map[string]any{})
		return
	}
	quick := values.Get("quick")
	if quick == "" {
		quick = "ALL"
	}
	if quick != "ALL" && quick != "ATTENTION" && quick != "REQUIRED" && quick != "OPTIONAL" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "BIOS 快速筛选无效", map[string]any{})
		return
	}
	status := values.Get("status")
	if status != "" && status != "MATCHED" && status != "MISSING" && status != "HASH_WARNING" &&
		status != "MISSING_ENTRY" && status != "OPTIONAL_MISSING" && status != "INVALID" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "BIOS 状态无效", map[string]any{})
		return
	}
	limit := 100
	if values.Get("limit") != "" {
		limit, _ = strconv.Atoi(values.Get("limit"))
	}
	scopeSQL := biosScopeSQL(scope)
	conditions := []string{"requirement.enabled=1", scopeSQL}
	arguments := make([]any, 0, 12)
	if value := strings.TrimSpace(values.Get("q")); value != "" {
		conditions = append(conditions, "(instr(lower(requirement.logical_name),lower(?))>0 OR instr(lower(core.name),lower(?))>0)")
		arguments = append(arguments, value, value)
	}
	for _, filter := range []struct{ name, column string }{{"coreId", "requirement.core_id"}, {"coreArtifactId", "requirement.core_artifact_id"}} {
		if value := values.Get(filter.name); value != "" {
			conditions = append(conditions, filter.column+"=?")
			arguments = append(arguments, value)
		}
	}
	if value := values.Get("platformId"); value != "" {
		conditions = append(conditions, "EXISTS(SELECT 1 FROM platform_cores platform_core WHERE platform_core.core_id=requirement.core_id AND platform_core.platform_id=?)")
		arguments = append(arguments, value)
	}
	statusExpression := "COALESCE(installation.status,CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 'OPTIONAL_MISSING' ELSE 'MISSING' END)"
	if status != "" {
		conditions = append(conditions, statusExpression+"=?")
		arguments = append(arguments, status)
	}
	switch quick {
	case "ATTENTION":
		conditions = append(conditions, "((requirement.requirement_mode<>'OPTIONAL' AND "+statusExpression+" IN ('MISSING','MISSING_ENTRY','INVALID')) OR "+statusExpression+"='HASH_WARNING')")
	case "REQUIRED":
		conditions = append(conditions, "requirement.requirement_mode='REQUIRED'")
	case "OPTIONAL":
		conditions = append(conditions, "requirement.requirement_mode='OPTIONAL'")
	}
	filterDigest := cursor.FilterDigest(map[string]any{"scope": scope, "q": strings.TrimSpace(values.Get("q")), "platformId": values.Get("platformId"), "coreId": values.Get("coreId"), "coreArtifactId": values.Get("coreArtifactId"), "status": status, "quick": quick})
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminBIOS", filterDigest, "BIOS_CATALOG_ASC")
		if err != nil || len(payload.SortValues) != 2 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		conditions = append(conditions, "(core.name>? OR (core.name=? AND requirement.logical_name>?) OR (core.name=? AND requirement.logical_name=? AND requirement.id>?))")
		arguments = append(arguments, payload.SortValues[0], payload.SortValues[0], payload.SortValues[1], payload.SortValues[0], payload.SortValues[1], payload.ID)
	}
	scopeCounts, summary, filtered, err := server.biosAggregates(request, scopeSQL)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	query := `
SELECT requirement.id,requirement.core_id,core.name,requirement.core_artifact_id,requirement.logical_name,
requirement.source_kind,requirement.requirement_mode,requirement.condition_code,requirement.md5,requirement.enabled,
requirement.version,` + statusExpression + `,installation.id,installation.md5,installation.sha1,installation.sha256,
installation.validated_requirement_version,installation.created_at_ms
FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id AND installation.is_active=1
WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY core.name COLLATE BINARY,requirement.logical_name COLLATE BINARY,requirement.id COLLATE BINARY LIMIT ?`
	pageArguments := append(append([]any{}, arguments...), limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, pageArguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0, limit+1)
	sortNames := make([][3]string, 0, limit+1)
	for rows.Next() {
		var id, coreID, coreName, artifactID, logicalName, sourceKind, mode, itemStatus string
		var condition, expectedMD5, installationID, installedMD5, installedSHA1, installedSHA256 sql.NullString
		var validatedVersion, installedAt sql.NullInt64
		var enabled int
		var version int64
		if err := rows.Scan(&id, &coreID, &coreName, &artifactID, &logicalName, &sourceKind, &mode, &condition, &expectedMD5, &enabled, &version, &itemStatus, &installationID, &installedMD5, &installedSHA1, &installedSHA256, &validatedVersion, &installedAt); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{"id": id, "coreId": coreID, "coreName": coreName, "coreArtifactId": artifactID, "logicalName": logicalName, "sourceKind": sourceKind, "requirementMode": mode, "conditionCode": nullableString(condition), "expectedMd5": nullableString(expectedMD5), "enabled": enabled == 1, "version": version, "status": itemStatus, "activeInstallation": nullableBIOSInstallation(installationID, installedMD5, installedSHA1, installedSHA256, validatedVersion, installedAt)})
		sortNames = append(sortNames, [3]string{coreName, logicalName, id})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		sortNames = sortNames[:limit]
		last := sortNames[len(sortNames)-1]
		token, encodeErr := server.cursors.Encode(cursor.Payload{OperationID: "getAdminBIOS", FilterDigest: filterDigest, SortCode: "BIOS_CATALOG_ASC", SortValues: []string{last[0], last[1]}, ID: last[2]})
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"generatedAtMs": server.now().UnixMilli(), "scope": scope, "scopeCounts": scopeCounts, "summary": summary, "filteredCount": filtered, "items": items, "nextCursor": next})
}

//nolint:lll // The EXISTS clause is a fixed library-scope predicate shared by list and aggregate queries.
func biosScopeSQL(scope string) string {
	if scope == "FULL_CATALOG" {
		return "1=1"
	}
	return `EXISTS(SELECT 1 FROM game_variants variant JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id JOIN games game ON game.id=variant.game_id WHERE revision.core_artifact_id=requirement.core_artifact_id AND game.status='PUBLISHED')`
}

//nolint:lll // Aggregate SQL expressions remain aligned with the status projection above.
func (server *Server) biosAggregates(
	request *http.Request,
	scopeSQL string,
) (map[string]int64, map[string]int64, int64, error) {
	var requiredByLibrary, fullCatalog int64
	if err := server.database.QueryRowContext(request.Context(), `SELECT COALESCE(sum(CASE WHEN `+biosScopeSQL("REQUIRED_BY_LIBRARY")+` THEN 1 ELSE 0 END),0),count(*) FROM bios_requirements requirement WHERE requirement.enabled=1`).Scan(&requiredByLibrary, &fullCatalog); err != nil {
		return nil, nil, 0, fmt.Errorf("aggregate BIOS scopes: %w", err)
	}
	counts := map[string]int64{"requiredByLibrary": requiredByLibrary, "fullCatalog": fullCatalog}
	statusExpression := "COALESCE(installation.status,CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 'OPTIONAL_MISSING' ELSE 'MISSING' END)"
	var total, blocking, warning, ready, attention, required, optional int64
	if err := server.database.QueryRowContext(request.Context(), `SELECT count(*),COALESCE(sum(CASE WHEN requirement.requirement_mode<>'OPTIONAL' AND `+statusExpression+` IN ('MISSING','MISSING_ENTRY','INVALID') THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN `+statusExpression+`='HASH_WARNING' THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN `+statusExpression+`='MATCHED' THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN (requirement.requirement_mode<>'OPTIONAL' AND `+statusExpression+` IN ('MISSING','MISSING_ENTRY','INVALID')) OR `+statusExpression+`='HASH_WARNING' THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN requirement.requirement_mode='REQUIRED' THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 1 ELSE 0 END),0) FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id AND installation.is_active=1 WHERE requirement.enabled=1 AND `+scopeSQL).Scan(&total, &blocking, &warning, &ready, &attention, &required, &optional); err != nil {
		return nil, nil, 0, fmt.Errorf("aggregate BIOS status: %w", err)
	}
	summary := map[string]int64{"totalCount": total, "blockingCount": blocking, "warningCount": warning, "readyCount": ready, "attentionCount": attention, "requiredCount": required, "optionalCount": optional}
	// Rebuild only the non-cursor arguments for the filtered aggregate.
	values := request.URL.Query()
	args := []any{}
	conditions := []string{"requirement.enabled=1", scopeSQL}
	if value := strings.TrimSpace(values.Get("q")); value != "" {
		conditions = append(conditions, "(instr(lower(requirement.logical_name),lower(?))>0 OR instr(lower(core.name),lower(?))>0)")
		args = append(args, value, value)
	}
	for _, filter := range []struct{ name, column string }{{"coreId", "requirement.core_id"}, {"coreArtifactId", "requirement.core_artifact_id"}} {
		if value := values.Get(filter.name); value != "" {
			conditions = append(conditions, filter.column+"=?")
			args = append(args, value)
		}
	}
	if value := values.Get("platformId"); value != "" {
		conditions = append(conditions, "EXISTS(SELECT 1 FROM platform_cores platform_core WHERE platform_core.core_id=requirement.core_id AND platform_core.platform_id=?)")
		args = append(args, value)
	}
	if value := values.Get("status"); value != "" {
		conditions = append(conditions, statusExpression+"=?")
		args = append(args, value)
	}
	switch values.Get("quick") {
	case "ATTENTION":
		conditions = append(conditions, "((requirement.requirement_mode<>'OPTIONAL' AND "+statusExpression+" IN ('MISSING','MISSING_ENTRY','INVALID')) OR "+statusExpression+"='HASH_WARNING')")
	case "REQUIRED":
		conditions = append(conditions, "requirement.requirement_mode='REQUIRED'")
	case "OPTIONAL":
		conditions = append(conditions, "requirement.requirement_mode='OPTIONAL'")
	}
	var filtered int64
	err := server.database.QueryRowContext(request.Context(), `SELECT count(*) FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id AND installation.is_active=1 WHERE `+strings.Join(conditions, " AND "), args...).Scan(&filtered)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, 0, fmt.Errorf("aggregate filtered BIOS catalog: %w", err)
	}
	return counts, summary, filtered, nil
}
