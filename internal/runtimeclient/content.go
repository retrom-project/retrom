package runtimeclient

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
)

type Locator struct {
	Path      string `json:"path"`
	Offset    int64  `json:"offset"`
	SizeBytes int64  `json:"sizeBytes"`
}

func (c *Client) StorageLayout(ctx context.Context, directory model.Directory, config json.RawMessage) (string, error) {
	var result struct {
		Layout string `json:"layout"`
	}
	err := c.Call(ctx, "storage-layout", map[string]any{
		"platformId": directory.PlatformID, "coreId": directory.DefaultCoreID, "config": config,
	}, &result)
	if err != nil {
		return "", err
	}
	if !model.OneOf(result.Layout, "FILES", "ZIP_STORE") {
		return "", model.ErrInvalid
	}
	return result.Layout, nil
}
