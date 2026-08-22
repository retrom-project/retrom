//go:build integration

package arcadedat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestRealDATStatisticsMatchManifest(t *testing.T) {
	t.Parallel()
	manifestPath := filepath.Join("..", "..", "data", "dat", "emulatorjs", "4.2.3", "manifest.json")
	contents, err := os.ReadFile(manifestPath)
	testassert.Falsef(t, err != nil, "read manifest: %v", err)
	var manifest struct {
		Cores []struct {
			CoreID string `json:"core_id"`
			DAT    *struct {
				LocalPath string `json:"local_path"`
			} `json:"dat"`
			ParseStats Stats `json:"parse_stats"`
		} `json:"cores"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	for _, core := range manifest.Cores {
		if core.DAT == nil {
			continue
		}
		core := core
		t.Run(core.CoreID, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(core.DAT.LocalPath))
			file, err := os.Open(path)
			testassert.Falsef(t, err != nil, "open DAT: %v", err)
			defer func() { cleanup.Error("close", file.Close()) }()
			actual, err := Parse(context.Background(), file, core.CoreID)
			testassert.Falsef(t, err != nil, "Parse() error = %v", err)
			testassert.Truef(t, reflect.DeepEqual(actual, core.ParseStats), "stats mismatch\nactual: %+v\nwant:   %+v", actual, core.ParseStats)
		})
	}
}
