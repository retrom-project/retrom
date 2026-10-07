package scans

import (
	"encoding/json"
	"testing"
)

func TestDOSReplacementRetainsExplicitProgramOrCoreMenu(t *testing.T) {
	t.Parallel()
	next := json.RawMessage(`{"content":{"kind":"DOS_BUNDLE","entryFile":"new.zip","entryPath":"ONLY.EXE"},"cores":{"dosbox_pure":{"options":{"cpuType":"auto"}}}}`)
	for _, previous := range []string{
		`{"content":{"kind":"DOS_BUNDLE","entryFile":"old.zip","entryPath":"no-longer-present/OLD.COM"}}`,
		`{"content":{"kind":"DOS_BUNDLE","entryFile":"old.zip"}}`,
	} {
		result, err := retainDOSProgram(json.RawMessage(previous), next)
		if err != nil {
			t.Fatal(err)
		}
		var oldConfig, updated struct {
			Content map[string]string
			Cores   map[string]any
		}
		if err = json.Unmarshal([]byte(previous), &oldConfig); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(result, &updated); err != nil {
			t.Fatal(err)
		}
		if updated.Content["entryPath"] != oldConfig.Content["entryPath"] || updated.Content["entryFile"] != "new.zip" {
			t.Fatalf("program choice changed: %s", result)
		}
		if updated.Cores["dosbox_pure"] == nil {
			t.Fatalf("new config options lost: %s", result)
		}
	}
}

func TestOtherReplacementContentIsUnchanged(t *testing.T) {
	t.Parallel()
	next := json.RawMessage(`{"content":{"kind":"SINGLE_FILE","entryFile":"new.nes"}}`)
	result, err := retainDOSProgram(json.RawMessage(`{"content":{"kind":"SINGLE_FILE","entryFile":"old.nes"}}`), next)
	if err != nil || string(result) != string(next) {
		t.Fatalf("non-DOS replacement changed: %s %v", result, err)
	}
}
