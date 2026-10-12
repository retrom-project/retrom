package runtimeclient

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
)

type ROMSetSelection struct {
	DATSHA256 string `json:"datSHA256"`
}
type ReconciledConfig struct {
	Config             json.RawMessage `json:"config"`
	RemovedParentFiles []string        `json:"removedParentFiles"`
}

func runtimeDirectory(directory model.Directory) map[string]any {
	return map[string]any{
		"platformId": directory.PlatformID, "defaultCoreId": directory.DefaultCoreID,
		"allowedCoreIds": directory.CoreIDs,
	}
}

func (c *Client) ROMSets(ctx context.Context, directory model.Directory, config json.RawMessage,
	files []model.GameFile, query model.ROMSetQuery,
) (model.ROMSetOptions, error) {
	var result model.ROMSetOptions
	input := map[string]any{
		"directory": runtimeDirectory(directory), "config": config, "files": Files(files),
		"coreId": query.CoreID, "query": query.Search, "offset": query.Offset, "limit": query.Limit,
	}
	if query.EntryFile != "" {
		input["entryFile"] = query.EntryFile
	}
	if query.ROMSet != nil {
		input["romSet"] = *query.ROMSet
	}
	err := c.Call(ctx, "rom-sets", input, &result)
	if err == nil && (result.CoreID != query.CoreID || result.Entries == nil || len(result.DATSHA256) != 64) {
		return result, model.ErrUnavailable
	}
	return result, err
}

func (c *Client) ReconcileConfig(ctx context.Context, directory model.Directory, config, previous json.RawMessage,
	files []model.GameFile,
) (ReconciledConfig, error) {
	var result ReconciledConfig
	err := c.Call(ctx, "reconcile-config", map[string]any{
		"directory": runtimeDirectory(directory), "config": config,
		"previousConfig": previous, "files": Files(files), "providerRoots": c.ProviderRoots(),
	}, &result)
	if err == nil && (len(result.Config) == 0 || result.RemovedParentFiles == nil) {
		return result, model.ErrUnavailable
	}
	return result, err
}
