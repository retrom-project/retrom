package runtimeclient

import (
	"strings"
	"testing"
)

func TestProviderModulesWithoutRun(t *testing.T) {
	bundle, revision := strings.Repeat("a", 64), strings.Repeat("b", 64)
	client := &Client{
		Providers: map[string]Provider{
			"z-provider": {ProviderID: "z-provider", ProviderVersion: "1.0.0", BundleSHA256: bundle, ModuleSHA256: revision, Root: "/private/z"},
			"a-provider": {ProviderID: "a-provider", ProviderVersion: "1.0.0", BundleSHA256: bundle, ModuleSHA256: revision, Root: "/private/a"},
		},
		Overrides: map[string]map[string]DevelopmentAsset{"a-provider": {"client.mjs": {}}},
	}
	items := client.ProviderModules()
	if len(items) != 2 || items[0].ProviderID != "a-provider" || items[1].ProviderID != "z-provider" {
		t.Fatalf("unexpected modules: %#v", items)
	}
	if items[0].ModuleURL != "/runtime/providers/a-provider/"+bundle+"/dev/"+revision+"/client.mjs" ||
		items[1].ModuleURL != "/runtime/providers/z-provider/"+bundle+"/client.mjs" {
		t.Fatalf("module URLs do not match installed/development assets: %#v", items)
	}
	if empty := (&Client{}).ProviderModules(); empty == nil || len(empty) != 0 {
		t.Fatalf("expected an empty list, got %#v", empty)
	}
}
