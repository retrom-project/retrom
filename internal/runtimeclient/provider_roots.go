package runtimeclient

// ProviderRoots contains only verified installed/development Provider locations.
func (c *Client) ProviderRoots() map[string]string {
	roots := make(map[string]string, len(c.Providers))
	for id, provider := range c.Providers {
		roots[id] = provider.Root
	}
	return roots
}
