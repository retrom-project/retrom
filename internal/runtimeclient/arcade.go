package runtimeclient

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/model"
)

type ArcadeParents struct {
	model.ArcadeParentOptions
	Config json.RawMessage `json:"config"`
}

func (c *Client) ArcadeParents(ctx context.Context, directory model.Directory, config json.RawMessage,
	files []model.GameFile, core, attachment string,
) (ArcadeParents, error) {
	var result ArcadeParents
	locators := make(map[string]string, len(files))
	for _, file := range files {
		if c.Locate == nil {
			return result, model.ErrUnavailable
		}
		absolute, err := c.Locate(file.StorageKey)
		if err != nil {
			return result, fmt.Errorf("parent file locator: %w", err)
		}
		locators[file.LogicalKey] = absolute
	}
	input := map[string]any{
		"directory": map[string]any{
			"platformId": directory.PlatformID, "defaultCoreId": directory.DefaultCoreID, "allowedCoreIds": directory.CoreIDs,
		},
		"config": config, "files": Files(files), "locators": locators, "coreId": core,
	}
	if attachment != "" {
		input["attachFile"] = attachment
	}
	if err := c.Call(ctx, "arcade-parents", input, &result); err != nil {
		return result, err
	}
	if result.CoreID != core || result.ParentFiles == nil || result.MissingParents == nil || len(result.Config) == 0 {
		return result, model.ErrUnavailable
	}
	return result, nil
}
