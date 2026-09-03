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
pc.enabled,
binding.provider_id,
binding.target_id,
target.netplay_compatibility_line
FROM platforms p
LEFT JOIN platform_cores pc ON pc.platform_id=p.id
LEFT JOIN cores c ON c.id=pc.core_id
LEFT JOIN runtime_binding_platforms binding_platform
 ON binding_platform.platform_id=p.id AND binding_platform.core_id=pc.core_id
LEFT JOIN runtime_target_bindings binding ON binding.binding_id=binding_platform.binding_id
 AND binding.launch_policy!='DISABLED'
LEFT JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
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
	coresByPlatformID := make(map[string]map[string]map[string]any)
	for rows.Next() {
		var id, name string
		var sortOrder, enabled int
		var coreID, coreName, providerID, targetID, netplayLine sql.NullString
		var coreEnabled sql.NullInt64
		if err := rows.Scan(
			&id, &name, &sortOrder, &enabled, &coreID, &coreName, &coreEnabled,
			&providerID, &targetID, &netplayLine,
		); err != nil {
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
			coresByPlatformID[id] = make(map[string]map[string]any)
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
			netplaySupported := providerID.Valid && targetID.Valid && netplayLine.Valid &&
				server.netplay.SupportsPlatformTarget(
					id, coreID.String, providerID.String, targetID.String, netplayLine.String,
				)
			if core := coresByPlatformID[id][coreID.String]; core != nil {
				if netplaySupported {
					core["netplaySupported"] = true
				}
				continue
			}
			core := map[string]any{
				"id": coreID.String, "name": coreName.String, "enabled": coreEnabled.Int64 == 1,
				"netplaySupported": netplaySupported,
			}
			coresByPlatformID[id][coreID.String] = core
			item["cores"] = append(
				cores,
				core,
			)
		}
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) runtimeTargets(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT provider.provider_id,
provider.provider_version,
provider.provider_api_version,
provider.bundle_sha256,
target.target_id,
target.display_name,
target.game_compatibility_line,
target.netplay_compatibility_line,
target.target_contract_sha256,
binding.core_id,
c.name,
binding.launch_policy
FROM runtime_providers provider
JOIN runtime_targets target ON target.provider_id=provider.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
JOIN cores c ON c.id=binding.core_id
ORDER BY provider.provider_id,target.target_id
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var providerID, providerVersion, bundleSHA256, targetID, displayName string
		var gameLine, targetContractSHA256, coreID, coreName, launchPolicy string
		var providerAPIVersion int
		var netplayLine sql.NullString
		if err := rows.Scan(
			&providerID, &providerVersion, &providerAPIVersion, &bundleSHA256,
			&targetID, &displayName, &gameLine, &netplayLine, &targetContractSHA256,
			&coreID, &coreName, &launchPolicy,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"providerId": providerID, "providerVersion": providerVersion,
				"providerApiVersion": providerAPIVersion, "bundleSha256": bundleSHA256,
				"targetId": targetID, "displayName": displayName,
				"gameCompatibilityLine": gameLine, "netplayCompatibilityLine": nullableString(netplayLine),
				"targetContractSha256": targetContractSHA256,
				"coreId":               coreID, "coreName": coreName, "launchPolicy": launchPolicy,
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
COALESCE((SELECT json_object(
  'schemaVersion',1,
  'supportedContentKinds',json((SELECT json_group_array(content_kind) FROM (
    SELECT content_kind FROM runtime_binding_content_kinds kinds
    WHERE kinds.binding_id=binding.binding_id ORDER BY content_kind
  ))),
  'multiDisc',CASE WHEN EXISTS(
    SELECT 1 FROM runtime_binding_content_kinds kinds
    WHERE kinds.binding_id=binding.binding_id AND kinds.content_kind='MULTI_DISC_M3U_V1'
  ) THEN json_object('maxDiscs',8,'maxTotalBytes',1073741824,'delivery','EAGER_EXTERNAL_FILES') ELSE NULL END
 )
 FROM runtime_target_bindings binding
 JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
  AND binding_platform.platform_id=pi.platform_id AND binding_platform.core_id=pi.default_core_id
 WHERE binding.core_id=pi.default_core_id AND binding.launch_policy<>'DISABLED'
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
