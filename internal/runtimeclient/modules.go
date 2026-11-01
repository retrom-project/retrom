package runtimeclient

import (
	"slices"

	"retrom/internal/model"
)

func (c *Client) ProviderBaseURL(provider Provider) string {
	base := "/runtime/providers/" + provider.ProviderID + "/" + provider.BundleSHA256 + "/"
	if len(c.Overrides[provider.ProviderID]) != 0 {
		base += "dev/" + provider.ModuleSHA256 + "/"
	}
	return base
}

func (c *Client) ProviderModules() []model.RuntimeProviderModule {
	ids := make([]string, 0, len(c.Providers))
	for id := range c.Providers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	items := make([]model.RuntimeProviderModule, 0, len(ids))
	for _, id := range ids {
		provider := c.Providers[id]
		base := c.ProviderBaseURL(provider)
		items = append(items, model.RuntimeProviderModule{
			ProviderID: provider.ProviderID, ProviderVersion: provider.ProviderVersion, ProviderAPIVersion: 1,
			BundleSHA256: provider.BundleSHA256, ModuleSHA256: provider.ModuleSHA256,
			ModuleURL: base + "client.mjs", RuntimeBaseURL: base,
		})
	}
	return items
}
