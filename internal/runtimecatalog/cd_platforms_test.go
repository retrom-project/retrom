package runtimecatalog

import (
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/platformcatalog"
)

func TestDiscAndBroadcastPlatformsHaveRecommendedBindings(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ platform, core, target string }{
		{"segacd", "genesis_plus_gx", "genesis-plus-gx-cd"},
		{"amigacd32", "puae", "puae"},
		{"satellaview", "snes9x", "snes9x"},
	} {
		if matches := defaultBindings(catalog, tc.platform, tc.core); len(matches) != 1 || matches[0].TargetID != tc.target {
			t.Errorf("%s/%s bindings = %#v", tc.platform, tc.core, matches)
		}
		found := false
		for _, template := range platformcatalog.Current().Templates {
			if template.PlatformID == tc.platform && template.DefaultCoreID == tc.core {
				found = true
			}
		}
		if !found {
			t.Errorf("missing recommended directory for %s/%s", tc.platform, tc.core)
		}
	}
}
