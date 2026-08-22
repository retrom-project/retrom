package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
)

func (server *Server) platforms(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT p.id,
p.name,
p.sort_order,
p.enabled,
pc.core_id,
c.name,
pc.enabled
FROM platforms p
LEFT JOIN platform_cores pc ON pc.platform_id=p.id
LEFT JOIN cores c ON c.id=pc.core_id
ORDER BY p.sort_order,
pc.core_id
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	byID := make(map[string]map[string]any)
	for rows.Next() {
		var id, name string
		var sortOrder, enabled int
		var coreID, coreName sql.NullString
		var coreEnabled sql.NullInt64
		if err := rows.Scan(&id, &name, &sortOrder, &enabled, &coreID, &coreName, &coreEnabled); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		item := byID[id]
		if item == nil {
			item = map[string]any{
				"id":        id,
				"name":      name,
				"sortOrder": sortOrder,
				"enabled":   enabled == 1,
				"cores":     []map[string]any{},
			}
			byID[id] = item
			items = append(items, item)
		}
		if coreID.Valid {
			cores, ok := item["cores"].([]map[string]any)
			if !ok {
				writeError(
					writer,
					request,
					http.StatusInternalServerError,
					"INTERNAL_ERROR",
					"平台核心投影无效",
					map[string]any{},
				)
				return
			}
			item["cores"] = append(
				cores,
				map[string]any{"id": coreID.String, "name": coreName.String, "enabled": coreEnabled.Int64 == 1},
			)
		}
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) coreArtifacts(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT a.id,
a.core_id,
c.name,
a.emulatorjs_version,
a.bundle_version,
a.flavor,
a.enabled,
a.version,
a.size_bytes
FROM core_artifacts a
JOIN cores c ON c.id=a.core_id
ORDER BY c.name,
a.emulatorjs_version,
a.id
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, coreID, coreName, ejsVersion, bundleVersion, flavor string
		var enabled int
		var version, size int64
		if err := rows.Scan(
			&id,
			&coreID,
			&coreName,
			&ejsVersion,
			&bundleVersion,
			&flavor,
			&enabled,
			&version,
			&size,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"id":                id,
				"coreId":            coreID,
				"coreName":          coreName,
				"emulatorjsVersion": ejsVersion,
				"bundleVersion":     bundleVersion,
				"flavor":            flavor,
				"enabled":           enabled == 1,
				"version":           version,
				"sizeBytes":         size,
			},
		)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func platformInstanceFilters(values url.Values) ([]string, []any, bool) {
	conditions := []string{"pi.deleted_at_ms IS NULL"}
	arguments := make([]any, 0, 2)
	if value := values.Get("platformId"); value != "" {
		conditions = append(conditions, "pi.platform_id=?")
		arguments = append(arguments, value)
	}
	if value := values.Get("enabled"); value != "" {
		if value != "true" && value != "false" {
			return nil, nil, false
		}
		conditions = append(conditions, "pi.enabled=?")
		arguments = append(arguments, map[string]int{"true": 1, "false": 0}[value])
	}
	return conditions, arguments, true
}

func (server *Server) platformInstances(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	conditions, arguments, ok := platformInstanceFilters(values)
	if !ok {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "目录启用状态无效", map[string]any{})
		return
	}
	query := queryWithConditions(
		`
SELECT pi.id,
pi.platform_id,
p.name,
pi.default_core_id,
c.name,
pi.name,
pi.slug,
pi.description,
pi.sort_order,
pi.enabled,
pi.version,
pi.updated_at_ms,
(SELECT count(*) FROM games g WHERE g.platform_instance_id=pi.id)
,
COALESCE((SELECT a.compatibility_config_json
 FROM core_artifacts a
 WHERE a.core_id=pi.default_core_id
 AND a.enabled=1
 LIMIT 1),'{}')
FROM platform_instances pi
JOIN platforms p ON p.id=pi.platform_id
JOIN cores c ON c.id=pi.default_core_id
`,
		conditions,
		` ORDER BY pi.sort_order,pi.id LIMIT 100`,
	)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, platformID, platformName, coreID, coreName, name, slug, description, compatibility string
		var sortOrder, enabled int
		var version, updatedAtMS, gameCount int64
		if err := rows.Scan(
			&id,
			&platformID,
			&platformName,
			&coreID,
			&coreName,
			&name,
			&slug,
			&description,
			&sortOrder,
			&enabled,
			&version,
			&updatedAtMS,
			&gameCount,
			&compatibility,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "platformId": platformID, "platformName": platformName, "defaultCoreId": coreID,
			"defaultCoreName": coreName, "name": name, "slug": slug, "description": description,
			"sortOrder": sortOrder, "enabled": enabled == 1, "version": version, "updatedAtMs": updatedAtMS,
			"gameCount": gameCount, "supportedExtensions": contentprofile.SupportedExtensions(platformID),
			"importCapabilities": contentcapability.Resolve(
				platformID, enabled == 1, server.config.MultiDiscImportEnabled, compatibility,
			),
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func nullableInteger(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func discLabel(value sql.NullInt64) any {
	if value.Valid {
		return fmt.Sprintf("光盘 %d", value.Int64+1)
	}
	return nil
}

func nullableBIOSInstallation(
	id, md5Value, sha1Value, sha256Value sql.NullString,
	validatedVersion, installedAt sql.NullInt64,
) any {
	if !id.Valid {
		return nil
	}
	return map[string]any{
		"id":                          id.String,
		"md5":                         md5Value.String,
		"sha1":                        sha1Value.String,
		"sha256":                      sha256Value.String,
		"validatedRequirementVersion": validatedVersion.Int64,
		"createdAtMs":                 installedAt.Int64,
	}
}
