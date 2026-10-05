package config

import (
	"fmt"
	"os"
	"strconv"
)

func loadSourceWorkers() (map[string]int, error) {
	workers := make(map[string]int, 2)
	for _, setting := range []struct {
		kind, variable string
		count          int
	}{
		{"IMPORT_SCAN", "RETROM_SOURCE_SCAN_WORKERS", 2},
		{"IMPORT_RECEIVE", "RETROM_SOURCE_IMPORT_WORKERS", 1},
	} {
		count := setting.count
		if raw := os.Getenv(setting.variable); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 32 {
				return nil, fmt.Errorf("%w: %s must be between 1 and 32", errInvalidConfig, setting.variable)
			}
			count = parsed
		}
		workers[setting.kind] = count
	}
	return workers, nil
}
