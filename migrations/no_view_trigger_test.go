package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var forbiddenViewOrTriggerDDL = regexp.MustCompile(`(?is)\b(?:create|alter|drop)\s+(?:view|trigger)\b`)

func TestMigrationsDoNotDefineViewsOrTriggers(t *testing.T) {
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
		if match := forbiddenViewOrTriggerDDL.Find(contents); match != nil {
			t.Errorf("%s contains forbidden view/trigger DDL: %q", path, match)
		}
	}
}
