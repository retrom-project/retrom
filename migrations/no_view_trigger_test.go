package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var (
	forbiddenViewDDL = regexp.MustCompile(`(?is)\b(?:create|alter|drop)\s+view\b`)
	triggerDDL       = regexp.MustCompile(`(?is)\b(?:create|alter|drop)\s+trigger\b`)
)

func TestMigrationsOnlyDefineBlobReferenceTriggers(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if match := forbiddenViewDDL.Find(contents); match != nil {
			t.Errorf("%s contains forbidden view DDL: %q", path, match)
		}
		if path != "016_blob_reference_counts.sql" {
			if match := triggerDDL.Find(contents); match != nil {
				t.Errorf("%s contains unexpected trigger DDL: %q", path, match)
			}
		}
	}
}
