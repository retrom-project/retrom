//go:build linux

package scummvm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	scummvmpolicy "retrom/internal/capability/engine/scummvm"
	model "retrom/internal/model/libraryimport"

	"golang.org/x/sys/unix"
)

const oldGoldenSHA256 = "416cc74c5f62ec7727261610f7351c3c340468c9b2242d8e6e54a79ab1ebb54b"

var errResolverFailure = errors.New("capture resolver failure")

type controlledContext struct {
	context.Context
	done    chan struct{}
	outcome error
}

func (ctx *controlledContext) Done() <-chan struct{} { return ctx.done }

func (ctx *controlledContext) Err() error {
	select {
	case <-ctx.done:
		return ctx.outcome
	default:
		return nil
	}
}

// The native fixture receives exactly the production argv and environment. It
// dispatches before testing parses flags, so no fixture flag or env is injected.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--config=/dev/null" {
		compatibilityChild()
		return
	}
	os.Exit(m.Run())
}

func TestNativeDetectorMatchesOldGoCompatibilityGolden(t *testing.T) {
	contents, err := os.ReadFile("testdata/old-go-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if compatibilitySum(contents) != oldGoldenSHA256 {
		t.Fatal("old implementation golden changed")
	}
	var golden struct {
		Cases map[string]json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(contents, &golden); err != nil {
		t.Fatal(err)
	}
	scenarios := compatibilityScenarios()
	if len(scenarios) != 53 || len(golden.Cases) != len(scenarios) {
		t.Fatalf("scenario coverage changed: actual=%d golden=%d", len(scenarios), len(golden.Cases))
	}
	sandbox := t.TempDir()
	compatibilityMkdir(t, filepath.Join(sandbox, "temporary"))
	// observe changes TMPDIR per case; retain the process's original environment.
	t.Setenv("TMPDIR", os.Getenv("TMPDIR"))
	for index, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			want, ok := golden.Cases[scenario.name]
			if !ok {
				t.Fatalf("missing old observation for %s", scenario.name)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, want); err != nil {
				t.Fatal(err)
			}
			got := compatibilityRaw(compatibilityObserve(t, sandbox, scenario, index))
			if !bytes.Equal(got, compact.Bytes()) {
				t.Errorf("old Go observation changed\nwant: %s\ngot:  %s", compact.Bytes(), got)
			}
		})
	}
}

func compatibilitySum(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func compatibilityRaw(value any) []byte {
	contents, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return contents
}

func compatibilityEncoded(value any) map[string]any {
	contents := compatibilityRaw(value)
	return map[string]any{"json": string(contents), "sha256": compatibilitySum(contents)}
}

func compatibilityError(err error, sandbox string) map[string]any {
	text, kind := "", ""
	if err != nil {
		text, kind = err.Error(), fmt.Sprintf("%T", err)
	}
	return map[string]any{
		"text": strings.ReplaceAll(text, sandbox, "<sandbox>"), "type": kind,
		"tool":     errors.Is(err, model.ErrScummVMToolFailed),
		"input":    errors.Is(err, model.ErrScummVMInputInvalid),
		"limit":    errors.Is(err, model.ErrScummVMLimit),
		"result":   errors.Is(err, scummvmpolicy.ErrResultInvalid),
		"canceled": errors.Is(err, context.Canceled),
		"deadline": errors.Is(err, context.DeadlineExceeded),
		"resolver": errors.Is(err, errResolverFailure), "notExist": errors.Is(err, os.ErrNotExist),
	}
}

func compatibilityGame(root, language string) scummvmpolicy.DetectedGame {
	return scummvmpolicy.DetectedGame{
		Root: root, EngineID: "sky", GameID: "sky", Description: "Fixture <&> 游戏",
		PreferredTarget: "sky", Language: language, Platform: "pc", Extra: "Floppy",
		GUIOptions: "", Config: map[string]string{}, CanBeAdded: true,
	}
}

func compatibilityResponse(candidates []scummvmpolicy.DetectedGame) []byte {
	return compatibilityRaw(map[string]any{
		"schemaVersion": 1, "upstreamCommit": testCommit, "candidates": candidates, "error": nil,
	})
}

func compatibilityChild() {
	var root, save string
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--path=") {
			root = strings.TrimPrefix(arg, "--path=")
		}
		if strings.HasPrefix(arg, "--savepath=") {
			save = strings.TrimPrefix(arg, "--savepath=")
		}
	}
	parent := filepath.Dir(root)
	mode, err := os.ReadFile(filepath.Join(parent, "mode"))
	if err != nil {
		panic(err)
	}
	compatibilityAuditChild(parent, root, save)
	switch string(mode) {
	case "cancel", "deadline":
		compatibilityBlockChild(parent)
	case "stdout-limit":
		if _, err := os.Stdout.Write([]byte(strings.Repeat("x", 16*1024*1024+1))); err != nil {
			os.Exit(7)
		}
		os.Exit(7)
	case "stderr-limit":
		if _, err := os.Stderr.Write([]byte(strings.Repeat("diagnostic", 7000))); err != nil {
			panic(err)
		}
	}
	contents, err := os.ReadFile(filepath.Join(parent, "response.json"))
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(contents); err != nil {
		panic(err)
	}
	if string(mode) == "exit7" {
		os.Exit(7)
	}
}

func compatibilityAuditChild(parent, root, save string) {
	env := os.Environ()
	sort.Strings(env)
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	args := append([]string(nil), os.Args[1:]...)
	for i, arg := range args {
		args[i] = strings.ReplaceAll(strings.ReplaceAll(arg, root, "<input>"), save, "<run>")
	}
	normalizedEnv := make([]string, len(env))
	for i, value := range env {
		normalizedEnv[i] = strings.ReplaceAll(value, save, "<run>")
	}
	info, err := os.Stat(save)
	if err != nil {
		panic(err)
	}
	audit := map[string]any{
		"args": args, "env": normalizedEnv, "cwdIsSavepath": cwd == save,
		"homeIsSavepath": os.Getenv("HOME") == save, "xdgIsSavepath": os.Getenv("XDG_CONFIG_HOME") == save,
		"directoryMode": uint32(info.Mode().Perm()),
	}
	if err := os.WriteFile(filepath.Join(parent, "invocation.json"), compatibilityRaw(audit), 0o600); err != nil {
		panic(err)
	}
}

func compatibilityBlockChild(parent string) {
	blocker, err := os.OpenFile(filepath.Join(parent, "block"), os.O_RDWR, 0o600)
	if err != nil {
		panic(err)
	}
	// Native cancellation kills this process while blocked on an owned FIFO.
	ready, err := os.OpenFile(filepath.Join(parent, "ready"), os.O_WRONLY, 0o600)
	if err != nil {
		panic(err)
	}
	if _, err := ready.Write([]byte("R")); err != nil {
		panic(err)
	}
	if err := ready.Close(); err != nil {
		panic(err)
	}
	_, readErr := blocker.Read(make([]byte, 1))
	closeErr := blocker.Close()
	panic(fmt.Sprintf("blocking helper unexpectedly resumed: %v", errors.Join(readErr, closeErr)))
}

type compatibilityScenario struct {
	name                                                    string
	contents                                                []byte
	mode, rootKind, toolKind, digest                        string
	canceled, resolverError, cancelInResolver, badTemporary bool
	engines                                                 []string
}

func compatibilityObserve(t *testing.T, sandbox string, scenario compatibilityScenario, index int) map[string]any {
	t.Helper()
	parent := filepath.Join(sandbox, fmt.Sprintf("case-%03d", index))
	compatibilityMkdir(t, parent)
	root := compatibilityInput(t, parent, scenario.rootKind)
	if scenario.contents == nil {
		scenario.contents = compatibilityResponse([]scummvmpolicy.DetectedGame{compatibilityGame("Game", "en")})
	}
	compatibilityWrite(t, filepath.Join(parent, "mode"), []byte(scenario.mode), 0o600)
	compatibilityWrite(t, filepath.Join(parent, "response.json"), scenario.contents, 0o600)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if scenario.canceled {
		cancel()
	}
	active, finish := compatibilityBarrier(ctx, t, parent, scenario.mode)
	calls := 0
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var detector model.ScummVMDetector = New(func(context.Context) (Tool, error) {
		calls++
		if scenario.cancelInResolver {
			cancel()
		}
		if scenario.resolverError {
			return Tool{}, errResolverFailure
		}
		return compatibilityTool(scenario, executable, parent), nil
	})
	temporary := filepath.Join(sandbox, "temporary")
	if scenario.badTemporary {
		temporary = filepath.Join(parent, "missing-temporary")
	}
	if err := os.Setenv("TMPDIR", temporary); err != nil {
		t.Fatal(err)
	}
	digest := scenario.digest
	if digest == "" {
		digest = strings.Repeat("a", 64)
	}
	result, detectErr := detector.Detect(active, root, digest)
	finish()
	record := map[string]any{
		"error": compatibilityError(detectErr, sandbox), "result": compatibilityEncoded(result),
		"resolverCalls": calls, "inputJSON": string(scenario.contents),
	}
	compatibilityObserveInvocation(t, parent, record)
	entries, err := os.ReadDir(filepath.Join(sandbox, "temporary"))
	if err != nil {
		t.Fatal(err)
	}
	record["temporaryEmpty"] = len(entries) == 0
	if detectErr == nil {
		compatibilityObserveSnapshot(result, sandbox, record)
	}
	return record
}

func compatibilityTool(scenario compatibilityScenario, executable, parent string) Tool {
	engines := scenario.engines
	if engines == nil {
		engines = []string{"sky"}
	}
	tool := Tool{Path: executable, UpstreamCommit: testCommit, Engines: engines}
	switch scenario.toolKind {
	case "relative":
		tool.Path = "relative"
	case "bad-commit":
		tool.UpstreamCommit = strings.Repeat("A", 40)
	case "empty-engines":
		tool.Engines = nil
	case "missing":
		tool.Path = filepath.Join(parent, "missing-executable")
	}
	return tool
}

func compatibilityInput(t *testing.T, parent, kind string) string {
	t.Helper()
	root := filepath.Join(parent, "input")
	compatibilityMkdir(t, root)
	compatibilityWrite(t, filepath.Join(root, "public.txt"), []byte("public fixture"), 0o400)
	switch kind {
	case "relative":
		return "relative"
	case "missing":
		return filepath.Join(parent, "missing")
	case "regular":
		return filepath.Join(root, "public.txt")
	case "symlink-root":
		target := root
		root = filepath.Join(parent, "link")
		if err := os.Symlink(target, root); err != nil {
			t.Fatal(err)
		}
	case "nested-symlink":
		if err := os.Symlink(filepath.Join(root, "public.txt"), filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	case "fifo":
		if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func compatibilityBarrier(ctx context.Context, t *testing.T, parent, mode string) (context.Context, func()) {
	t.Helper()
	if mode != "cancel" && mode != "deadline" {
		return ctx, func() {}
	}
	for _, name := range []string{"ready", "block"} {
		if err := unix.Mkfifo(filepath.Join(parent, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manual := &controlledContext{Context: ctx, done: make(chan struct{}), outcome: context.Canceled}
	if mode == "deadline" {
		manual.outcome = context.DeadlineExceeded
	}
	ready, err := os.OpenFile(filepath.Join(parent, "ready"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	readyResult := make(chan error, 1)
	go func() {
		_, readErr := ready.Read(make([]byte, 1))
		close(manual.done)
		readyResult <- readErr
	}()
	return manual, func() {
		closeErr := ready.Close()
		if err := errors.Join(<-readyResult, closeErr); err != nil {
			t.Fatal(err)
		}
	}
}

func compatibilityObserveInvocation(t *testing.T, parent string, record map[string]any) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(parent, "invocation.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var audit map[string]any
	if err := json.Unmarshal(contents, &audit); err != nil {
		t.Fatal(err)
	}
	record["invocation"] = audit
}

func compatibilityObserveSnapshot(result scummvmpolicy.Result, sandbox string, record map[string]any) {
	snapshot, err := scummvmpolicy.NewSnapshot(result)
	record["snapshotError"], record["snapshot"] = compatibilityError(err, sandbox), compatibilityEncoded(snapshot)
	if err != nil {
		return
	}
	state, code := snapshot.Status()
	record["status"] = []string{state, code}
	selected, err := snapshot.Selected()
	record["selected"], record["selectionError"] = compatibilityEncoded(selected), compatibilityError(err, sandbox)
	parsed, err := scummvmpolicy.ParseSnapshot(string(compatibilityRaw(snapshot)))
	record["reparsed"], record["reparseError"] = compatibilityEncoded(parsed), compatibilityError(err, sandbox)
	selections := map[string]any{}
	for _, candidate := range result.Candidates {
		value, err := snapshot.Select(candidate.ID)
		selections[candidate.ID] = map[string]any{
			"snapshot": compatibilityEncoded(value), "error": compatibilityError(err, sandbox),
		}
	}
	record["selections"] = selections
}

func compatibilityMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func compatibilityWrite(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func compatibilityScenarios() []compatibilityScenario {
	valid := compatibilityGame("Game", "en")
	tests := []compatibilityScenario{
		{name: "single"},
		{name: "empty", contents: compatibilityResponse([]scummvmpolicy.DetectedGame{})},
		{name: "ambiguous-language", contents: compatibilityResponse([]scummvmpolicy.DetectedGame{compatibilityGame("Game", "en"), compatibilityGame("Game", "de")})},
		{name: "multiple-roots", contents: compatibilityResponse([]scummvmpolicy.DetectedGame{compatibilityGame("Z", "en"), compatibilityGame("A", "en")})},
		{name: "engine-unavailable", engines: []string{"queen"}},
		{name: "source-digest-identity", digest: strings.Repeat("b", 64)},
		{name: "invalid-digest-first", digest: "bad", rootKind: "missing", resolverError: true, canceled: true},
		{name: "invalid-root-before-cancel", rootKind: "missing", canceled: true},
		{name: "canceled-tree", canceled: true},
		{name: "resolver-error", resolverError: true},
		{name: "relative-root", rootKind: "relative"},
		{name: "regular-root", rootKind: "regular"},
		{name: "symlink-root", rootKind: "symlink-root"},
		{name: "nested-symlink", rootKind: "nested-symlink"},
		{name: "special-file", rootKind: "fifo"},
		{name: "relative-tool", toolKind: "relative"},
		{name: "invalid-tool-commit", toolKind: "bad-commit"},
		{name: "empty-tool-engines", toolKind: "empty-engines"},
		{name: "missing-executable", toolKind: "missing"},
		{name: "tool-exit-7", mode: "exit7"},
		{name: "stderr-overflow", mode: "stderr-limit"},
		{name: "stdout-overflow-precedes-exit", mode: "stdout-limit"},
		{name: "cancel-after-start", mode: "cancel"},
		{name: "deadline-after-start", mode: "deadline"},
		{name: "cancel-in-resolver", cancelInResolver: true},
		{name: "temporary-failure-before-cancel", cancelInResolver: true, badTemporary: true},
		{name: "invalid-json", contents: []byte("{")},
		{name: "trailing-json", contents: append(compatibilityResponse([]scummvmpolicy.DetectedGame{valid}), []byte(" {}")...)},
		{name: "unknown-top-level", contents: []byte(strings.Replace(string(compatibilityResponse([]scummvmpolicy.DetectedGame{valid})), "\"candidates\":", "\"unknown\":1,\"candidates\":", 1))},
		{name: "null-candidates", contents: compatibilityResponse(nil)},
		{name: "duplicate-candidates", contents: compatibilityResponse([]scummvmpolicy.DetectedGame{valid, valid})},
		{name: "raw-commit-mismatch", contents: []byte(strings.ReplaceAll(string(compatibilityResponse([]scummvmpolicy.DetectedGame{valid})), testCommit, strings.Repeat("b", 40)))},
		{name: "raw-error-field", contents: []byte(strings.Replace(string(compatibilityResponse([]scummvmpolicy.DetectedGame{valid})), "\"error\":null", "\"error\":\"upstream-failure\"", 1))},
		{name: "duplicate-json-key-last-wins", contents: []byte(strings.Replace(string(compatibilityResponse([]scummvmpolicy.DetectedGame{valid})), "\"schemaVersion\":1", "\"schemaVersion\":2,\"schemaVersion\":1", 1))},
		{name: "trailing-whitespace", contents: append(compatibilityResponse([]scummvmpolicy.DetectedGame{valid}), []byte("  \n\t")...)},
		{name: "invalid-utf8-description", contents: []byte(strings.Replace(string(compatibilityResponse([]scummvmpolicy.DetectedGame{valid})), "游戏", string([]byte{0xff}), 1))},
	}

	candidates := compatibilityCandidateScenarios()
	combined := make([]compatibilityScenario, len(tests)+len(candidates))
	copy(combined, tests)
	copy(combined[len(tests):], candidates)
	return combined
}

func compatibilityCandidateScenarios() []compatibilityScenario {
	variants := []struct {
		name   string
		change func(*scummvmpolicy.DetectedGame)
	}{
		{"unknown-variant", func(candidate *scummvmpolicy.DetectedGame) { candidate.HasUnknownFiles = true }},
		{"not-addable", func(candidate *scummvmpolicy.DetectedGame) { candidate.CanBeAdded = false }},
		{"addon", func(candidate *scummvmpolicy.DetectedGame) { candidate.IsAddOn = true }},
		{"unsupported-level", func(candidate *scummvmpolicy.DetectedGame) { candidate.SupportLevel = 3 }},
		{"unknown-priority", func(candidate *scummvmpolicy.DetectedGame) {
			candidate.HasUnknownFiles = true
			candidate.CanBeAdded = false
		}},
		{"unsafe-root", func(candidate *scummvmpolicy.DetectedGame) { candidate.Root = "../escape" }},
		{"unsafe-filename", func(candidate *scummvmpolicy.DetectedGame) { candidate.Config["filename"] = "../escape" }},
		{"nil-config", func(candidate *scummvmpolicy.DetectedGame) { candidate.Config = nil }},
		{"unknown-config", func(candidate *scummvmpolicy.DetectedGame) { candidate.Config["arbitrary"] = "yes" }},
		{"hint-control", func(candidate *scummvmpolicy.DetectedGame) { candidate.Description = "bad\ncontrol" }},
		{"root-over-limit", func(candidate *scummvmpolicy.DetectedGame) { candidate.Root = strings.Repeat("x", 2049) }},
		{"language-over-limit", func(candidate *scummvmpolicy.DetectedGame) { candidate.Language = strings.Repeat("x", 129) }},
		{"description-over-limit", func(candidate *scummvmpolicy.DetectedGame) { candidate.Description = strings.Repeat("x", 4097) }},
		{"filename-over-limit", func(candidate *scummvmpolicy.DetectedGame) { candidate.Config["filename"] = strings.Repeat("x", 241) }},
		{"invalid-engine", func(candidate *scummvmpolicy.DetectedGame) { candidate.EngineID = "UPPER" }},
		{"invalid-game", func(candidate *scummvmpolicy.DetectedGame) { candidate.GameID = "with space" }},
		{"support-over-limit", func(candidate *scummvmpolicy.DetectedGame) { candidate.SupportLevel = 5 }},
	}
	tests := make([]compatibilityScenario, 0, len(variants))
	for _, variant := range variants {
		candidate := compatibilityGame("Game", "en")
		variant.change(&candidate)
		tests = append(tests, compatibilityScenario{
			name: variant.name, contents: compatibilityResponse([]scummvmpolicy.DetectedGame{candidate}),
		})
	}
	return tests
}
