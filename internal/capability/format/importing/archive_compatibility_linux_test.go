package importing_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const archiveGoldenSHA256 = "d3ec7fbf0f83bc84d20a9fe06258203c4daf4f371d8de3ea0d0dcade659be9d4"

func TestArchiveCompatibilityGolden(t *testing.T) {
	golden := loadArchiveGolden(t)
	sourceRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Clean(filepath.Join(sourceRoot, "../../../.."))
	inputs := compatFixtures(t.TempDir())
	cases := append(compatArchiveCases(inputs), compatSevenZipCases(sourceRoot, inputs)...)
	actual := make([]result, 0, len(golden.Cases))
	for _, test := range cases {
		outcome := runCase(test)
		outcome.Error = strings.ReplaceAll(outcome.Error, inputs.dir, "<fixture>")
		outcome.Error = strings.ReplaceAll(outcome.Error, repo, "<repo>")
		actual = append(actual, outcome)
	}
	actual = append(actual, compatWorkerCases(t, filepath.Join(sourceRoot, "testdata/sevenzip/single.7z"))...)
	if len(actual) != 64 || len(golden.Cases) != 64 {
		t.Fatalf("case count actual=%d expected=%d", len(actual), len(golden.Cases))
	}
	for index, want := range golden.Cases {
		t.Run(want.Name, func(t *testing.T) {
			if !reflect.DeepEqual(actual[index], want) {
				gotJSON, _ := json.Marshal(actual[index])
				wantJSON, _ := json.Marshal(want)
				t.Fatalf("legacy behavior changed\ngot: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
	if children := compatWorkerChildren(t); children != golden.ChildrenAfter {
		t.Fatalf("workers remain after capture: %q", children)
	}
	if !reflect.DeepEqual(inputs.hashes, golden.FixtureSHA256) {
		t.Fatalf("deterministic fixture bytes changed: got %v want %v", inputs.hashes, golden.FixtureSHA256)
	}
}

func loadArchiveGolden(t *testing.T) capture {
	t.Helper()
	directory := "testdata/archive-compatibility"
	body, err := os.ReadFile(filepath.Join(directory, "old-go-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if sha(body) != archiveGoldenSHA256 {
		t.Fatal("frozen old-Go golden SHA changed")
	}
	var golden capture
	if err := json.Unmarshal(body, &golden); err != nil {
		t.Fatal(err)
	}
	for name, digest := range golden.SourceSHA256 {
		body, err := os.ReadFile(filepath.Join(directory, "old-source", filepath.Base(name)+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		if sha(body) != digest {
			t.Fatalf("old source %s has changed", name)
		}
	}
	var provenance struct {
		GoldenSHA  string `json:"sha256"`
		CaptureSHA string `json:"capture_source_sha256"`
		Cases      int    `json:"cases"`
	}
	body, err = os.ReadFile(filepath.Join(directory, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &provenance); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(directory, "old-capture.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if sha(body) != provenance.CaptureSHA || provenance.GoldenSHA != archiveGoldenSHA256 || provenance.Cases != 64 {
		t.Fatal("frozen capture provenance changed")
	}
	return golden
}

func compatWorkerCases(t *testing.T, path string) []result {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	arguments := [][]string{
		{"scan", "20000", "8589934592", "34359738368", "200", "false"},
		{"extract-batch", "0"},
		{"extract-batch", "01"},
		{"extract-batch", "0,0"},
		{"extract-batch", ""},
		{"extract", "-1"},
		{"scan", "20001", "8589934592", "34359738368", "200", "false"},
		{"unknown"},
	}
	outcomes := make([]result, 0, len(arguments))
	for _, args := range arguments {
		outcomes = append(outcomes, protocol(ctx, executable, path, args))
	}
	return outcomes
}

func compatWorkerChildren(t *testing.T) string {
	t.Helper()
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, task := range tasks {
		children, err := os.ReadFile(filepath.Join("/proc/self/task", task.Name(), "children"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range strings.Fields(string(children)) {
			seen[child] = true
		}
	}
	children := make([]string, 0, len(seen))
	for child := range seen {
		children = append(children, child)
	}
	sort.Strings(children)
	return strings.Join(children, " ")
}
