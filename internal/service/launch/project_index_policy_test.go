package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/engine/scummvm"
)

func TestProjectIndexesPreservesFormatDocumentsAndPreviewOrder(t *testing.T) {
	t.Parallel()
	cases := []struct{ format, raw, marker, font string }{
		{"ONS_PROJECT", `{"schemaVersion":1,"ons":{"markerPath":"nscript.dat","fontPath":"default.ttf","scriptEncoding":"utf8"}}`, "nscript.dat", "default.ttf"},
		{"KIRIKIRI_PROJECT", `{"schemaVersion":1,"kirikiri":{"markerPath":"startup.tjs","startupXp3Path":null,"compatibility":"KAG_RUNTIME_TRIAL_REQUIRED"}}`, "startup.tjs", ""},
		{"BUTTERSCOTCH_PROJECT", `{"schemaVersion":1,"butterscotch":{"markerPath":"data.win","compatibility":"GAMEMAKER_RUNTIME_TRIAL_REQUIRED"}}`, "data.win", ""},
		{"NXENGINE_PROJECT", `{"schemaVersion":1,"nxengine":{"markerPath":"Doukutsu.exe","compatibility":"NXENGINE_RUNTIME_TRIAL_REQUIRED"}}`, "Doukutsu.exe", ""},
	}
	for _, item := range cases {
		t.Run(item.format, func(t *testing.T) {
			for _, preview := range []bool{false, true} {
				memory := indexMemoryFixture()
				memory.snapshot.Source.ContentKind, memory.snapshot.Source.DependencyJSON = item.format, item.raw
				memory.snapshot.Source.Title = "Retrom title"
				memory.snapshot.Files = []model.ProjectIndexRecord{
					{Content: model.ConfigFile{LogicalName: "! assets/first#.bin", Format: item.format, Digest: strings.Repeat("b", 64), Size: 3, Role: "PROJECT_FILE"}, Order: 2},
					{Content: model.ConfigFile{LogicalName: item.marker, Format: item.format, Digest: strings.Repeat("a", 64), Size: 16, Role: "GAME"}, Primary: preview},
				}
				if preview {
					memory.snapshot.Source.Purpose = "REVIEW_PREVIEW"
				}
				if item.font != "" {
					memory.snapshot.Files = append(memory.snapshot.Files, model.ProjectIndexRecord{
						Content: model.ConfigFile{LogicalName: item.font, Format: item.format, Digest: strings.Repeat("c", 64), Size: 8, Role: "RUNTIME_FILE"}, Order: 1,
					})
				}
				assertProjectIndexDocument(t, memory, preview, item.marker, item.font)
			}
		})
	}
}

func assertProjectIndexDocument(t *testing.T, memory *projectIndexMemory, preview bool, marker, font string) {
	t.Helper()
	before, err := json.Marshal(memory.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), model.ProjectIndexReference{ID: "session"}, "valid")
	if err != nil {
		t.Fatal(err)
	}
	var decoded onsProjectIndex
	if err := json.Unmarshal(result.Contents, &decoded); err != nil {
		t.Fatal(err)
	}
	first := "! assets/first#.bin"
	if preview {
		first = marker
	}
	if decoded.SchemaVersion != 1 || len(decoded.Files) != len(memory.snapshot.Files) || decoded.Files[0].Path != first || decoded.FontPath != font {
		t.Fatalf("preview=%t document=%s", preview, result.Contents)
	}
	if font != "" && decoded.Title != "Retrom title" {
		t.Fatalf("title lost: %s", result.Contents)
	}
	if !strings.Contains(string(result.Contents), "%21%20assets/first%23.bin") {
		t.Fatalf("path escaping: %s", result.Contents)
	}
	sum := sha256.Sum256(result.Contents)
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("digest differs from exact response bytes")
	}
	after, err := json.Marshal(memory.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("building index mutated repository snapshot")
	}
}

func TestProjectIndexesOnlyStaticFormatsPermitFallback(t *testing.T) {
	t.Parallel()
	memory := indexMemoryFixture()
	memory.snapshot.Source.ContentKind = "RPG_MAKER_PROJECT"
	memory.snapshot.Files[0].Content.Format = "RPG_MAKER_PROJECT"
	service := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) })
	result, err := service.Index(t.Context(), model.ProjectIndexReference{ID: "session"}, "valid")
	if !errors.Is(err, model.ErrProjectIndexUnavailable) || len(result.Contents) != 0 {
		t.Fatalf("static index=%v", err)
	}
	memory.snapshot.Files[0].Content.Digest = "corrupt"
	if _, err := service.Index(t.Context(), model.ProjectIndexReference{ID: "session"}, "valid"); !errors.Is(err, model.ErrCredential) || errors.Is(err, model.ErrProjectIndexUnavailable) {
		t.Fatalf("corruption fell back: %v", err)
	}
}

func TestProjectIndexesRetainsProfileFailure(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"ONS_PROJECT", "KIRIKIRI_PROJECT", "BUTTERSCOTCH_PROJECT", "NXENGINE_PROJECT", "SCUMMVM_PROJECT"} {
		memory := indexMemoryFixture()
		memory.snapshot.Source.ContentKind, memory.snapshot.Source.DependencyJSON = format, "{"
		memory.snapshot.Files[0].Content.Format = format
		_, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), model.ProjectIndexReference{ID: "session"}, "valid")
		if !errors.Is(err, model.ErrCredential) || errors.Is(err, model.ErrProjectIndexUnavailable) {
			t.Fatalf("%s corruption fell back: %v", format, err)
		}
		if format == "SCUMMVM_PROJECT" && !errors.Is(err, scummvm.ErrResultInvalid) {
			t.Fatalf("ScummVM parse cause lost: %v", err)
		}
	}
}

func TestProjectIndexesRejectsValidScummVMSnapshotWithoutSelectedGame(t *testing.T) {
	t.Parallel()
	snapshot, err := scummvm.NewSnapshot(scummvm.Result{
		UpstreamCommit: strings.Repeat("a", 40), SourceDigest: strings.Repeat("b", 64), Candidates: []scummvm.Candidate{}, Roots: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	memory := indexMemoryFixture()
	memory.snapshot.Source.ContentKind, memory.snapshot.Source.DependencyJSON = "SCUMMVM_PROJECT", string(raw)
	memory.snapshot.Files[0].Content.Format = "SCUMMVM_PROJECT"
	result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), model.ProjectIndexReference{}, "valid")
	if !errors.Is(err, model.ErrCredential) || !errors.Is(err, scummvm.ErrResultInvalid) || len(result.Contents) != 0 {
		t.Fatalf("unselected ScummVM index=%v", err)
	}
}

func TestProjectIndexesKeepsEngineEmptyFileRules(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		format, raw, path string
		allowed           bool
	}{
		{"KIRIKIRI_PROJECT", `{"schemaVersion":1,"kirikiri":{"markerPath":"startup.tjs","startupXp3Path":null,"compatibility":"KAG_RUNTIME_TRIAL_REQUIRED"}}`, "startup.tjs", true},
		{"BUTTERSCOTCH_PROJECT", `{"schemaVersion":1,"butterscotch":{"markerPath":"data.win","compatibility":"GAMEMAKER_RUNTIME_TRIAL_REQUIRED"}}`, "data.win", false},
		{"NXENGINE_PROJECT", `{"schemaVersion":1,"nxengine":{"markerPath":"Doukutsu.exe","compatibility":"NXENGINE_RUNTIME_TRIAL_REQUIRED"}}`, "Doukutsu.exe", false},
	} {
		memory := indexMemoryFixture()
		memory.snapshot.Source.ContentKind, memory.snapshot.Source.DependencyJSON = item.format, item.raw
		memory.snapshot.Files[0].Content.Format, memory.snapshot.Files[0].Content.LogicalName = item.format, item.path
		memory.snapshot.Files[0].Content.Size = 0
		result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), model.ProjectIndexReference{}, "valid")
		if item.allowed && (err != nil || len(result.Contents) == 0) {
			t.Fatalf("%s empty file rejected: %v", item.format, err)
		}
		if !item.allowed && (!errors.Is(err, model.ErrCredential) || len(result.Contents) != 0) {
			t.Fatalf("%s empty file accepted: %v", item.format, err)
		}
	}
}

func TestProjectIndexesRejectsNXEngineFileCountBeyondContract(t *testing.T) {
	t.Parallel()
	memory := indexMemoryFixture()
	for index := 0; index < 4095; index++ {
		file := memory.snapshot.Files[0]
		file.Content.LogicalName = fmt.Sprintf("data/%04d.bin", index)
		memory.snapshot.Files = append(memory.snapshot.Files, file)
	}
	service := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) })
	if _, err := service.Index(t.Context(), model.ProjectIndexReference{}, "valid"); err != nil {
		t.Fatalf("maximum permitted NXEngine tree rejected: %v", err)
	}
	file := memory.snapshot.Files[0]
	file.Content.LogicalName = "overflow.bin"
	memory.snapshot.Files = append(memory.snapshot.Files, file)
	result, err := service.Index(t.Context(), model.ProjectIndexReference{}, "valid")
	if !errors.Is(err, model.ErrCredential) || len(result.Contents) != 0 || result.SHA256 != "" {
		t.Fatalf("oversized NXEngine tree published: %v", err)
	}
}
