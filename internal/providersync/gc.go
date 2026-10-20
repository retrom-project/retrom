package providersync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CollectGarbage removes shared-cache chunks that no listed Provider root references
// through its completion marker. Roots without a marker keep nothing alive.
func CollectGarbage(cache string, roots []string) (int, error) {
	release, err := lock(cache)
	if err != nil {
		return 0, err
	}
	defer release()
	keep := make(map[string]bool)
	for _, root := range roots {
		raw, readErr := os.ReadFile(filepath.Join(root, "ready.json"))
		if os.IsNotExist(readErr) {
			continue
		}
		var marker readyMarker
		if readErr != nil || json.Unmarshal(raw, &marker) != nil || marker.SchemaVersion != 1 {
			return 0, fmt.Errorf("read marker of %s: %w", root, ErrInputsInvalid)
		}
		for _, provider := range marker.Providers {
			for _, chunk := range provider.Chunks {
				keep[chunk] = true
			}
		}
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		return 0, fmt.Errorf("list chunk cache: %w", err)
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !lowerDigest.MatchString(entry.Name()) || keep[entry.Name()] {
			continue
		}
		if err = os.RemoveAll(filepath.Join(cache, entry.Name())); err != nil {
			return removed, fmt.Errorf("remove chunk %s: %w", entry.Name(), err)
		}
		removed++
	}
	return removed, nil
}
