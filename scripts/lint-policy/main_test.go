package main

import (
	"os"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestGoLintPolicy(t *testing.T) {
	contents, err := os.ReadFile("../../.golangci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration struct {
		Linters struct {
			Enable     []string                  `yaml:"enable"`
			Settings   map[string]map[string]any `yaml:"settings"`
			Exclusions map[string]any            `yaml:"exclusions"`
		} `yaml:"linters"`
		Run struct {
			Tests bool `yaml:"tests"`
		} `yaml:"run"`
	}
	if err = yaml.Unmarshal(contents, &configuration); err != nil {
		t.Fatal(err)
	}
	wanted := []string{
		"asasalint", "bodyclose", "canonicalheader", "contextcheck", "copyloopvar", "depguard", "dogsled",
		"dupl", "err113", "errcheck", "errorlint", "exhaustive", "fatcontext", "forbidigo", "funlen",
		"gochecknoinits", "gocognit", "gocritic", "gocyclo", "govet", "ineffassign", "interfacebloat", "lll",
		"makezero", "misspell", "nakedret", "nestif", "nilerr", "nilnil", "noctx", "nolintlint",
		"nonamedreturns", "prealloc", "predeclared", "revive", "rowserrcheck", "sqlclosecheck", "staticcheck",
		"unconvert", "unparam", "unused", "whitespace", "wrapcheck",
	}
	for _, required := range wanted {
		found := false
		for _, enabled := range configuration.Linters.Enable {
			found = found || enabled == required
		}
		if !found {
			t.Errorf("required Go linter removed: %s", required)
		}
	}
	limits := map[string]map[string]any{
		"dupl": {"threshold": 120}, "funlen": {"lines": 150, "statements": 100},
		"gocognit": {"min-complexity": 25}, "gocyclo": {"min-complexity": 15},
		"interfacebloat": {"max": 10}, "lll": {"line-length": 120}, "nestif": {"min-complexity": 4},
	}
	for linter, settings := range limits {
		for key, maximum := range settings {
			if actual := configuration.Linters.Settings[linter][key]; !reflect.DeepEqual(actual, maximum) {
				t.Errorf("%s.%s=%v expected %v", linter, key, actual, maximum)
			}
		}
	}
	wantedExclusions := map[string]any{
		"generated": "strict", "warn-unused": true,
		"rules": []any{map[string]any{
			"path": `_test\.go`, "linters": []any{"dupl", "err113", "lll", "wrapcheck"},
		}},
	}
	if !configuration.Run.Tests || !reflect.DeepEqual(configuration.Linters.Exclusions, wantedExclusions) {
		t.Error("lint must include tests and retain the reviewed exclusion scope")
	}
}
