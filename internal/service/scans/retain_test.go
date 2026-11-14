package scans

import (
	"encoding/json"
	"testing"

	"retrom/internal/model"
)

func replacementFiles(keys ...string) []model.ContentFile {
	files := make([]model.ContentFile, 0, len(keys))
	for _, key := range keys {
		files = append(files, model.ContentFile{LogicalKey: key})
	}
	return files
}

func decodeConfig(t *testing.T, raw json.RawMessage) (map[string]string, map[string]map[string]map[string]any) {
	t.Helper()
	var config struct {
		Content map[string]string
		Cores   map[string]struct{ Options map[string]any }
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	cores := map[string]map[string]map[string]any{}
	for id, core := range config.Cores {
		cores[id] = map[string]map[string]any{"options": core.Options}
	}
	return config.Content, cores
}

func TestReplacementRetainsSelectedEntryAndCoreOptions(t *testing.T) {
	t.Parallel()
	previous := json.RawMessage(`{"cores":{"kirikiri2":{"options":{"engineMode":"KIRIKIRI_Z"}}},` +
		`"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"data/startup.tjs"}}`)
	next := json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"data.xp3"}}`)
	result, err := retainReviewerSelections(previous, next, replacementFiles("data.xp3", "data/startup.tjs"))
	if err != nil {
		t.Fatal(err)
	}
	content, cores := decodeConfig(t, result)
	if content["entryFile"] != "data/startup.tjs" || cores["kirikiri2"]["options"]["engineMode"] != "KIRIKIRI_Z" {
		t.Fatalf("reviewer selection lost: %s", result)
	}
}

func TestReplacementUsesNewEntryWhenSelectionIsGone(t *testing.T) {
	t.Parallel()
	previous := json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"dtcn.xp3"}}`)
	next := json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"data.xp3"}}`)
	result, err := retainReviewerSelections(previous, next, replacementFiles("data.xp3"))
	if err != nil || string(result) != string(next) {
		t.Fatalf("missing entry retained: %s %v", result, err)
	}
	kindChanged := json.RawMessage(`{"content":{"kind":"ONS_PROJECT","entryFile":"0.txt"}}`)
	result, err = retainReviewerSelections(previous, kindChanged, replacementFiles("dtcn.xp3", "0.txt"))
	if err != nil || string(result) != string(kindChanged) {
		t.Fatalf("selection crossed content kinds: %s %v", result, err)
	}
}

func TestReplacementRetainsBGIRootAndEncoding(t *testing.T) {
	t.Parallel()
	previous := json.RawMessage(`{"cores":{"ethornell":{"options":{"textEncoding":"gbk"}}},` +
		`"content":{"kind":"BGI_PROJECT","entryFile":"game/system.arc"}}`)
	next := json.RawMessage(`{"cores":{"ethornell":{"options":{"textEncoding":"cp932"}}},` +
		`"content":{"kind":"BGI_PROJECT","entryFile":"system.arc"}}`)
	result, err := retainReviewerSelections(previous, next, replacementFiles("system.arc", "game/system.arc"))
	if err != nil {
		t.Fatal(err)
	}
	content, cores := decodeConfig(t, result)
	if content["entryFile"] != "game/system.arc" || cores["ethornell"]["options"]["textEncoding"] != "gbk" {
		t.Fatalf("BGI reviewer selection lost: %s", result)
	}
}

func TestReplacementMergesOptionsWithoutCopyingFileReferences(t *testing.T) {
	t.Parallel()
	previous := json.RawMessage(`{"cores":{"fbneo":{"parentFiles":["old.zip"],"options":{"region":"jp"}}},` +
		`"content":{"kind":"ARCADE","entryFile":"game.zip"}}`)
	next := json.RawMessage(`{"cores":{"fbneo":{"parentFiles":["new.zip"],"options":{"region":"us","dips":"a"}}},` +
		`"content":{"kind":"ARCADE","entryFile":"other.zip"}}`)
	result, err := retainReviewerSelections(previous, next, replacementFiles("game.zip", "other.zip", "new.zip"))
	if err != nil {
		t.Fatal(err)
	}
	content, cores := decodeConfig(t, result)
	options := cores["fbneo"]["options"]
	if content["entryFile"] != "other.zip" || options["region"] != "jp" || options["dips"] != "a" {
		t.Fatalf("arcade replacement merged incorrectly: %s", result)
	}
	var raw struct {
		Cores map[string]struct{ ParentFiles []string }
	}
	if err = json.Unmarshal(result, &raw); err != nil || len(raw.Cores["fbneo"].ParentFiles) != 1 || raw.Cores["fbneo"].ParentFiles[0] != "new.zip" {
		t.Fatalf("parent files copied from previous content: %s %v", result, err)
	}
}

func TestReplacementRejectsMalformedSavedConfiguration(t *testing.T) {
	t.Parallel()
	next := json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"data.xp3"}}`)
	if _, err := retainReviewerSelections(json.RawMessage(`{"content":`), next, nil); err == nil {
		t.Fatal("malformed saved configuration accepted")
	}
	if _, err := retainReviewerSelections(json.RawMessage(`{"content":{"kind":"KIRIKIRI_PROJECT","entryFile":"a"},"cores":[]}`), next, nil); err == nil {
		t.Fatal("malformed saved cores accepted")
	}
}
