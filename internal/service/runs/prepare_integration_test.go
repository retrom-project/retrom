//go:build integration

package runs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/testsupport"
)

const originalOptions = `{"difficulty":"original"}`

func TestLaunchAndRestoreOptionsShareOneSaveProjection(t *testing.T) {
	t.Parallel()
	f, s, detail, directory := launchPreparationFixture(t)
	input := model.RunInput{GameID: f.Game.ID, Purpose: "play"}
	ordinary, noSave, err := s.prepare(t.Context(), f.Principal, input, detail, directory)
	assertOrdinaryPreparation(t, s.Runtime.Root, ordinary, noSave, err)
	save := model.Save{
		ID: uuid.NewString(), UserID: f.Principal.User.ID, Game: detail.Game, Kind: "checkpoint",
		Name: "Original options", StorageKey: "saves/original", PayloadHash: "original-payload",
		SizeBytes: 10, LastCommitID: uuid.NewString(), Extinfo: model.Extinfo{
			CoreID: ordinary.CoreID, ProviderID: ordinary.ProviderID, TargetID: ordinary.TargetID,
			CoreFingerprint: ordinary.CoreFingerprint, ROMHash: ordinary.ROMHash,
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
	assertSavedContext(t, recordedPrepare(t, s.Runtime.Root), save.Extinfo)
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

func launchPreparationFixture(t *testing.T) (testsupport.Fixture, *Service, model.GameDetail, model.Directory) {
	t.Helper()
	f := testsupport.Library(t)
	f.Game.Input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"SINGLE_FILE","entryFile":"game.nes"},` +
		`"cores":{"fceumm":{"options":{"difficulty":"current"}}}}`)
	f.Publish(t)
	client, _ := prepareRuntimeFixture(t)
	s := &Service{Repository: f.Repository, Runtime: client}
	detail, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := f.Repository.Directory(t.Context(), f.DirectoryID)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, detail, directory
}

func assertOrdinaryPreparation(t *testing.T, root string, prepared runtimeclient.Prepared, save *model.Save, err error) {
	t.Helper()
	if err != nil || save != nil || string(prepared.TargetOptions) != `{"difficulty":"current"}` {
		t.Fatalf("ordinary options=%s save=%v error=%v", prepared.TargetOptions, save, err)
	}
	if _, exists := recordedPrepare(t, root)["savedContext"]; exists {
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

func recordedPrepare(t *testing.T, root string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "prepare-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]json.RawMessage
	if err = json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func prepareRuntimeFixture(t *testing.T) (*runtimeclient.Client, string) {
	t.Helper()
	root := t.TempDir()
	writePrepareFile(t, root, "dist/runtime/index.js", []byte("test runtime tool entry"))
	writePrepareFile(t, root, "scripts/runtime-cli.mjs", []byte(prepareWorker))
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
	client, err := runtimeclient.Open(t.Context(), root, "python3", filepath.Join(root, "providers"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, root
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

// The child models only the runtime IPC boundary, not core execution. Runtime's
// own tests verify content/options interpretation and identity rejection.
const prepareWorker = `import json, pathlib, sys
root = pathlib.Path(__file__).resolve().parent.parent
for line in sys.stdin:
    request = json.loads(line)
    command, value = request['command'], request['input']
    if command == 'catalog':
        result = {'cores': [], 'platforms': [], 'bindings': [], 'providers': [], 'biosRequirements': []}
    elif command == 'prepare':
        (root / 'prepare-input.json').write_text(json.dumps(value))
        saved = value.get('savedContext')
        core = value.get('coreId', value['directory']['defaultCoreId'])
        config = value['config']
        options = saved['runtimeOptions'] if saved else config['cores'][core]['options']
        content = saved['content'] if saved else config['content']
        result = {'coreId': core, 'providerId': 'emulatorjs', 'targetId': core,
            'coreFingerprint': value['fingerprints']['emulatorjs/' + core],
            'romHash': value['files'][0]['sha256'], 'config': {'content': content},
            'targetOptions': options, 'capabilities': {}, 'resources': [], 'biosRequirements': [],
            'checkpoint': {'writeFormat': 'state', 'readFormats': ['state'], 'maxBytes': 1024, 'semantics': 'INSTANT'}}
    elif command == 'restorable':
        result = {'restorable': value['current']['runtimeOptions'] == value['context']['runtimeOptions'], 'reason': None}
    else:
        raise ValueError(command)
    print(json.dumps({'id': request['id'], 'result': result}, separators=(',', ':')), flush=True)
`
