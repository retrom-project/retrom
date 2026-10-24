package scans

import (
	"strings"
	"testing"
)

func testNormalizationPlugin() normalizationPlugin {
	return normalizationPlugin{
		ID: "format-family", Version: 1, Formats: []string{"format"},
		Sources:    []normalizationFileDigest{{File: "data.xp3", SizeBytes: 42, SHA256: strings.Repeat("a", 64)}},
		Outputs:    []normalizationFileDigest{{File: "data.xp3", SizeBytes: 43, SHA256: strings.Repeat("b", 64)}},
		Validation: normalizationValidation{OutputFilesVerified: 1, ResourceEntriesVerified: 3},
	}
}

func TestNormalizationPluginReceiptsAreRequiredAndBounded(t *testing.T) {
	t.Parallel()
	if err := validateNormalizationPlugins([]normalizationPlugin{testNormalizationPlugin()}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*normalizationPlugin){
		"missing id":       func(p *normalizationPlugin) { p.ID = "" },
		"missing version":  func(p *normalizationPlugin) { p.Version = 0 },
		"unknown format":   func(p *normalizationPlugin) { p.Formats = []string{"private/path"} },
		"duplicate format": func(p *normalizationPlugin) { p.Formats = []string{"format", "format"} },
		"missing source":   func(p *normalizationPlugin) { p.Sources = nil },
		"source path":      func(p *normalizationPlugin) { p.Sources[0].File = "/private/source" },
		"source hash":      func(p *normalizationPlugin) { p.Sources[0].SHA256 = "invalid" },
		"output path":      func(p *normalizationPlugin) { p.Outputs[0].File = "../output" },
		"missing outputs":  func(p *normalizationPlugin) { p.Outputs = nil },
		"unverified file":  func(p *normalizationPlugin) { p.Validation.OutputFilesVerified = 0 },
		"negative count":   func(p *normalizationPlugin) { p.Validation.ResourceEntriesVerified = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			plugin := testNormalizationPlugin()
			mutate(&plugin)
			if err := validateNormalizationPlugins([]normalizationPlugin{plugin}); err == nil {
				t.Fatal("invalid plugin receipt accepted")
			}
		})
	}
	for _, plugins := range [][]normalizationPlugin{nil, {testNormalizationPlugin(), testNormalizationPlugin()}, make([]normalizationPlugin, 17)} {
		if err := validateNormalizationPlugins(plugins); err == nil {
			t.Fatal("missing/duplicate/unbounded plugin receipt accepted")
		}
	}
}
