package httpapi

import (
	"net/http"
	"net/url"

	catalogservice "retrom/internal/service/catalog"
)

func (server *Server) platforms(writer http.ResponseWriter, request *http.Request) {
	platforms, err := server.catalogService.Platforms(request.Context())
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(platforms))
	for _, platform := range platforms {
		cores := make([]map[string]any, 0, len(platform.Cores))
		for _, core := range platform.Cores {
			cores = append(cores, map[string]any{
				"id": core.ID, "name": core.Name, "enabled": core.Enabled,
			})
		}
		items = append(items, map[string]any{
			"id": platform.ID, "name": platform.Name, "sortOrder": platform.SortOrder,
			"enabled": platform.Enabled, "cores": cores,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) runtimeTargets(writer http.ResponseWriter, request *http.Request) {
	targets, err := server.catalogService.RuntimeTargets(request.Context())
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		items = append(items, map[string]any{
			"providerId": target.ProviderID, "providerVersion": target.ProviderVersion,
			"providerApiVersion": target.ProviderAPIVersion, "bundleSha256": target.BundleSHA256,
			"targetId": target.TargetID, "displayName": target.DisplayName,
			"coreId": target.CoreID, "coreName": target.CoreName, "launchPolicy": target.LaunchPolicy,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func platformInstanceFilters(values url.Values) (catalogservice.PlatformInstanceQuery, bool) {
	query := catalogservice.PlatformInstanceQuery{}
	if value := values.Get("platformId"); value != "" {
		query.PlatformID = &value
	}
	if value := values.Get("enabled"); value != "" {
		if value != "true" && value != "false" {
			return catalogservice.PlatformInstanceQuery{}, false
		}
		enabled := value == "true"
		query.Enabled = &enabled
	}
	return query, true
}

func (server *Server) platformInstances(writer http.ResponseWriter, request *http.Request) {
	query, ok := platformInstanceFilters(request.URL.Query())
	if !ok {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "目录启用状态无效", map[string]any{})
		return
	}
	instances, err := server.catalogService.PlatformInstances(
		request.Context(), query, server.config.MultiDiscImportEnabled,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(instances))
	for _, instance := range instances {
		items = append(items, map[string]any{
			"id": instance.ID, "platformId": instance.PlatformID, "platformName": instance.PlatformName,
			"defaultCoreId": instance.DefaultCoreID, "defaultCoreName": instance.DefaultCoreName,
			"name": instance.Name, "slug": instance.Slug, "description": instance.Description,
			"sortOrder": instance.SortOrder, "enabled": instance.Enabled,
			"version": instance.Version, "updatedAtMs": instance.UpdatedAtMS,
			"gameCount": instance.GameCount, "supportedExtensions": instance.SupportedExtensions,
			"importCapabilities": instance.ImportCapabilities,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}
