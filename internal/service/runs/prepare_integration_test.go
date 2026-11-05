//go:build integration

package runs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/retrom-project/retrom-runtime/host"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/testsupport"
)

const originalOptions = `{"difficulty":"original"}`

func TestLaunchAndRestoreOptionsShareOneSaveProjection(t *testing.T) {
	t.Parallel()
	f, s, detail, directory, recorder := launchPreparationFixture(t)
	input := model.RunInput{GameID: f.Game.ID, Purpose: "play"}
	ordinary, noSave, err := s.prepare(t.Context(), f.Principal, input, detail, directory)
	assertOrdinaryPreparation(t, recorder, ordinary, noSave, err)
	save := model.Save{
		ID: uuid.NewString(), UserID: f.Principal.User.ID, Game: detail.Game, Kind: "checkpoint",
		Name: "Original options", StorageKey: "saves/original", PayloadHash: "original-payload",
		SizeBytes: 10, LastCommitID: uuid.NewString(), Extinfo: model.Extinfo{
			CoreID: ordinary.CoreID, ProviderID: ordinary.ProviderID, TargetID: ordinary.TargetID,
			CoreFingerprint: "old-core-fingerprint", ROMHash: ordinary.ROMHash,
			CheckpointFormat: ordinary.Checkpoint.WriteFormat, Content: json.RawMessage(`{"kind":"SINGLE_FILE",` +
				`"entryFile":"game.nes"}`), RuntimeOptions: json.RawMessage(originalOptions),
		},
	}
	if err = f.Repository.WriteSave(t.Context(), save, 2000, true); err != nil {
		t.Fatal(err)
	}
	input.SaveID, input.CoreID = save.ID, "different-requested-core"
	prepared, projection, err := s.prepare(t.Context(), f.Principal, input, detail, directory)
	if err != nil || projection == nil || string(prepared.TargetOptions) != originalOptions ||
		prepared.CoreID != save.Extinfo.CoreID {
		t.Fatalf("restored options=%s save=%v error=%v", prepared.TargetOptions, projection, err)
	}
	assertSavedContext(t, recordedPrepare(t, recorder), save.Extinfo)
	// A concurrent overwrite between preparation and envelope assembly must not
	// combine the previous context with a second read of the replacement payload.
	replacement := save
	replacement.Version, replacement.PayloadHash = 1, "replacement-payload"
	replacement.StorageKey, replacement.LastCommitID = "saves/replacement", uuid.NewString()
	replacement.Extinfo.RuntimeOptions = json.RawMessage(`{"difficulty":"replacement"}`)
	if err = f.Repository.WriteSave(t.Context(), replacement, 3000, false); err != nil {
		t.Fatal(err)
	}
	run := Context{
		Run:    model.Run{ID: uuid.NewString(), GameID: f.Game.ID, Purpose: "play"},
		UserID: f.Principal.User.ID, Indexes: make(map[string][]Blob), Checkpoint: prepared.Checkpoint,
	}
	if err = s.envelope(t.Context(), &run, detail, prepared, projection); err != nil {
		t.Fatal(err)
	}
	assertFrozenRestore(t, run.Run, save)
	assertPreparationWrites(t, f, detail, replacement)
}

func launchPreparationFixture(t *testing.T) (testsupport.Fixture, *Service, model.GameDetail, model.Directory,
	*prepareHost,
) {
	t.Helper()
	f := testsupport.Library(t)
	f.Game.Input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"SINGLE_FILE","entryFile":"game.nes"},` +
		`"cores":{"fceumm":{"options":{"difficulty":"current"}}}}`)
	f.Publish(t)
	client, recorder := prepareRuntimeFixture(t)
	s := &Service{Repository: f.Repository, Runtime: client}
	detail, err := f.Repository.GameRuntimeDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := f.Repository.Directory(t.Context(), f.DirectoryID)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, detail, directory, recorder
}

func assertOrdinaryPreparation(t *testing.T, recorder *prepareHost, prepared runtimeclient.Prepared, save *model.Save, err error) {
	t.Helper()
	if err != nil || save != nil || string(prepared.TargetOptions) != `{"difficulty":"current"}` {
		t.Fatalf("ordinary options=%s save=%v error=%v", prepared.TargetOptions, save, err)
	}
	if _, exists := recordedPrepare(t, recorder)["savedContext"]; exists {
		t.Fatal("ordinary launch sent a saved context")
	}
}

func assertPreparationWrites(t *testing.T, f testsupport.Fixture, detail model.GameDetail, replacement model.Save) {
	t.Helper()
	current, err := f.Repository.Save(t.Context(), f.Principal.User.ID, replacement.ID)
	if err != nil || current.Version != 2 || current.PayloadHash != replacement.PayloadHash {
		t.Fatalf("replacement not committed: save=%+v error=%v", current, err)
	}
	after, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil || after.Game.Version != detail.Game.Version || after.Game.ContentHash != detail.Game.ContentHash {
		t.Fatalf("run preparation mutated game: game=%+v error=%v", after.Game, err)
	}
	if count, countErr := f.Repository.Count(t.Context(), "SELECT count(*) FROM scan_progress_tab"); countErr != nil || count != 0 {
		t.Fatalf("run preparation wrote scan state: count=%d error=%v", count, countErr)
	}
}

func assertFrozenRestore(t *testing.T, run model.Run, original model.Save) {
	t.Helper()
	if run.Save == nil || run.Save.Version != 1 || run.Save.PayloadHash != original.PayloadHash ||
		run.Save.RestoreWarning != "core_changed" || run.Extinfo.CoreFingerprint == original.Extinfo.CoreFingerprint ||
		string(run.Extinfo.RuntimeOptions) != originalOptions {
		t.Fatalf("mixed restore projection: save=%+v options=%s", run.Save, run.Extinfo.RuntimeOptions)
	}
	var envelope struct {
		TargetOptions json.RawMessage `json:"targetOptions"`
		Restore       struct {
			SHA256 string `json:"sha256"`
		} `json:"restore"`
	}
	if err := json.Unmarshal(run.Envelope, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope.TargetOptions) != originalOptions || envelope.Restore.SHA256 != original.PayloadHash {
		t.Fatalf("mixed restore envelope: %s", run.Envelope)
	}
}

func assertSavedContext(t *testing.T, request map[string]json.RawMessage, expected model.Extinfo) {
	t.Helper()
	var actual model.Extinfo
	if err := json.Unmarshal(request["savedContext"], &actual); err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("saved context changed: actual=%s expected=%s", got, want)
	}
}

func recordedPrepare(t *testing.T, recorder *prepareHost) map[string]json.RawMessage {
	t.Helper()
	recorder.mutex.Lock()
	raw := recorder.prepared
	recorder.mutex.Unlock()
	var request map[string]json.RawMessage
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func prepareRuntimeFixture(t *testing.T) (*runtimeclient.Client, *prepareHost) {
	t.Helper()
	root := t.TempDir()
	fingerprint := hex.EncodeToString(make([]byte, 32))
	provider := runtimeclient.Provider{
		ProviderID: "emulatorjs", ProviderVersion: "test", BundleSHA256: fingerprint,
		ModuleSHA256: fingerprint, InstallationPath: "emulatorjs/" + fingerprint,
	}
	installed := filepath.Join(root, "providers/installed", provider.InstallationPath)
	files := map[string][]byte{
		"provider.json":             []byte(`{"providerId":"emulatorjs","targets":[{"id":"fceumm"}]}`),
		"runtime-fingerprints.json": []byte(`{"targets":{"fceumm":{"fingerprint":"` + fingerprint + `"}}}`),
	}
	entries := make([]map[string]any, 0, len(files))
	for name, raw := range files {
		writePrepareFile(t, installed, name, raw)
		hash := sha256.Sum256(raw)
		entries = append(entries, map[string]any{"path": name, "sha256": hex.EncodeToString(hash[:]), "sizeBytes": len(raw)})
	}
	writePrepareJSON(t, installed, "integrity.json", map[string]any{"files": entries})
	writePrepareJSON(t, root, "providers/active.json", map[string]any{"providers": []runtimeclient.Provider{provider}})
	recorder := &prepareHost{}
	client, err := runtimeclient.Open(t.Context(), filepath.Join(root, "providers"), runtimeclient.UseHost(recorder))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, recorder
}

func writePrepareJSON(t *testing.T, root, name string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writePrepareFile(t, root, name, raw)
}

func writePrepareFile(t *testing.T, root, name string, raw []byte) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// prepareHost models only the runtime host boundary, not core execution. Runtime's own tests verify
// content/options interpretation and identity rejection.
type prepareHost struct {
	mutex    sync.Mutex
	prepared json.RawMessage
}

type prepareValue struct {
	Directory struct {
		DefaultCoreID string `json:"defaultCoreId"`
	} `json:"directory"`
	CoreID string          `json:"coreId"`
	Config json.RawMessage `json:"config"`
	Files  []struct {
		SHA256 string `json:"sha256"`
	} `json:"files"`
	Fingerprints map[string]string `json:"fingerprints"`
	SavedContext *struct {
		RuntimeOptions json.RawMessage `json:"runtimeOptions"`
		Content        json.RawMessage `json:"content"`
	} `json:"savedContext"`
}

func (h *prepareHost) Call(_ context.Context, command string, input []byte) (json.RawMessage, error) {
	switch command {
	case "catalog":
		return json.RawMessage(`{"cores":[],"platforms":[],"bindings":[],"providers":[],"biosRequirements":[]}`), nil
	case "prepare":
		h.mutex.Lock()
		h.prepared = append(json.RawMessage(nil), input...)
		h.mutex.Unlock()
		return preparedResult(input)
	case "restorable":
		return restorableResult(input)
	}
	return nil, &host.Error{Code: "RUNTIME_COMMAND_INVALID"}
}

func preparedResult(input []byte) (json.RawMessage, error) {
	var value prepareValue
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, err
	}
	core := value.CoreID
	if core == "" {
		core = value.Directory.DefaultCoreID
	}
	var config struct {
		Content json.RawMessage `json:"content"`
		Cores   map[string]struct {
			Options json.RawMessage `json:"options"`
		} `json:"cores"`
	}
	if err := json.Unmarshal(value.Config, &config); err != nil {
		return nil, err
	}
	options, content := config.Cores[core].Options, config.Content
	if value.SavedContext != nil {
		options, content = value.SavedContext.RuntimeOptions, value.SavedContext.Content
	}
	return json.Marshal(map[string]any{
		"coreId": core, "providerId": "emulatorjs", "targetId": core,
		"coreFingerprint": value.Fingerprints["emulatorjs/"+core], "romHash": value.Files[0].SHA256,
		"config": map[string]any{"content": content}, "targetOptions": options, "capabilities": map[string]any{},
		"resources": []any{}, "biosRequirements": []any{},
		"checkpoint": map[string]any{
			"writeFormat": "state", "readFormats": []string{"state"}, "maxBytes": 1024,
			"semantics": "INSTANT",
		},
	})
}

func restorableResult(input []byte) (json.RawMessage, error) {
	var value struct {
		Current, Context struct {
			RuntimeOptions  any    `json:"runtimeOptions"`
			CoreFingerprint string `json:"coreFingerprint"`
		}
	}
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, err
	}
	var warning any
	if value.Current.CoreFingerprint != value.Context.CoreFingerprint {
		warning = "CORE_CHANGED"
	}
	return json.Marshal(map[string]any{
		"restorable": reflect.DeepEqual(value.Current.RuntimeOptions, value.Context.RuntimeOptions), "reason": nil,
		"warning": warning,
	})
}
