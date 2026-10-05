package config

import "testing"

func TestSourceWorkerConcurrencyConfiguration(t *testing.T) {
	t.Setenv("RETROM_SOURCE_SCAN_WORKERS", "")
	t.Setenv("RETROM_SOURCE_IMPORT_WORKERS", "")
	defaults, err := loadSourceWorkers()
	if err != nil || defaults["IMPORT_SCAN"] != 2 || defaults["IMPORT_RECEIVE"] != 1 {
		t.Fatalf("defaults=%v err=%v", defaults, err)
	}
	for _, variable := range []string{"RETROM_SOURCE_SCAN_WORKERS", "RETROM_SOURCE_IMPORT_WORKERS"} {
		t.Run(variable, func(t *testing.T) {
			for _, value := range []string{"0", "-1", "33", "many"} {
				t.Setenv(variable, value)
				if _, err := loadSourceWorkers(); err == nil {
					t.Fatalf("accepted %s=%s", variable, value)
				}
			}
			t.Setenv(variable, "3")
			result, err := loadSourceWorkers()
			if err != nil {
				t.Fatal(err)
			}
			kind := "IMPORT_SCAN"
			if variable == "RETROM_SOURCE_IMPORT_WORKERS" {
				kind = "IMPORT_RECEIVE"
			}
			if result[kind] != 3 {
				t.Fatalf("workers=%v", result)
			}
		})
	}
}
