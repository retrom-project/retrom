package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

const shapeRuntime = `(function (global) {
  "use strict";
  var generation = global.Utils.RPGMAKER_NAME;
  var state = { mapId: 1, x: 10, y: 8, fixtureState: 0 };
  var saved = {};
  function Scene_Map() {}
  global.Scene_Map = Scene_Map;
  global.$gameMap = { mapId: function () { return state.mapId; }, isEventRunning: function () { return false; } };
  global.$gamePlayer = {};
  Object.defineProperties(global.$gamePlayer, {
    x: { get: function () { return state.x; }, set: function (value) { state.x = value; } },
    y: { get: function () { return state.y; }, set: function (value) { state.y = value; } }
  });
  global.$gameVariables = {
    value: function (id) { return id === 1 ? state.fixtureState : 0; },
    setValue: function (id, value) { if (id === 1) state.fixtureState = Number(value) || 0; }
  };
  global.$gameMessage = { isBusy: function () { return false; } };
  global.SceneManager = {
    _scene: new Scene_Map(), _stopped: false,
    updateMain: function () { draw(); }, stop: function () { this._stopped = true; },
    resume: function () { this._stopped = false; }, requestUpdate: function () {},
    goto: function (Scene) { this._scene = new Scene(); }
  };
  function snapshot() { return { mapId: state.mapId, x: state.x, y: state.y, fixtureState: state.fixtureState }; }
  function restore(value) { state.mapId = value.mapId; state.x = value.x; state.y = value.y; state.fixtureState = value.fixtureState; }
  global.StorageManager = generation === "MZ" ? {
    saveObject: async function (name, value) { saved[name] = JSON.stringify(value); },
    loadObject: async function (name) { return JSON.parse(saved[name]); },
    exists: function (name) { return Object.prototype.hasOwnProperty.call(saved, name); }
  } : {
    save: function (id, value) { saved[String(id)] = String(value); },
    load: function (id) { return saved[String(id)]; },
    exists: function (id) { return Object.prototype.hasOwnProperty.call(saved, String(id)); }
  };
  global.DataManager = {
    _globalInfo: null, maxSavefiles: function () { return 20; }, isDatabaseLoaded: function () { return true; },
    saveGame: async function (slot) {
      if (generation === "MZ") {
        await global.StorageManager.saveObject("global", { fixture: true });
        await global.StorageManager.saveObject("file" + slot, snapshot());
      } else {
        global.StorageManager.save(0, JSON.stringify({ fixture: true }));
        global.StorageManager.save(slot, JSON.stringify(snapshot()));
      }
      return true;
    },
    loadGame: async function (slot) {
      var value = generation === "MZ" ? await global.StorageManager.loadObject("file" + slot) :
        JSON.parse(global.StorageManager.load(slot));
      restore(value); return true;
    }
  };
  global.WebAudio = { setMasterVolume: function () {} };
  var canvas = document.querySelector("canvas");
  var context = canvas.getContext("2d");
  function draw() {
    context.fillStyle = "#101827"; context.fillRect(0, 0, canvas.width, canvas.height);
    context.fillStyle = "#40d0ff"; context.fillRect(24, 24, 592, 5);
    context.fillStyle = "#ffffff"; context.font = "24px sans-serif";
    context.fillText("RETROM " + generation + " ISOLATION HARNESS", 24, 70);
    context.fillText("position " + state.x + "," + state.y + " state " + state.fixtureState, 24, 110);
    context.fillStyle = "#40d0ff"; context.fillRect(24 + state.x * 20, 150 + state.y * 12, 18, 18);
  }
  function sound() {
    try {
      var audio = new AudioContext(); var source = audio.createBufferSource();
      source.buffer = audio.createBuffer(1, 128, audio.sampleRate); source.connect(audio.destination); source.start();
    } catch (_) {}
  }
  global.addEventListener("keydown", function (event) {
    if (event.key === "ArrowRight") state.x += 1;
    if (event.key === "ArrowLeft") state.x -= 1;
    if (event.key === "ArrowDown") state.y += 1;
    if (event.key === "ArrowUp") state.y -= 1;
    if (event.key === "Enter" || event.key === " ") state.fixtureState = (state.fixtureState + 1) % 3;
    sound(); draw();
  });
  function tick() { if (!global.SceneManager._stopped) global.SceneManager.updateMain(); global.requestAnimationFrame(tick); }
  draw(); global.requestAnimationFrame(tick);
})(window);
`

const maliciousProbe = `(function (global) {
  "use strict";
  var results = Object.create(null);
  global.__RETROM_MALICIOUS_RESULTS__ = results;
  function record(name, outcome) { results[name] = String(outcome); }
  try { record("parentDom", global.parent.document.body ? "exposed" : "empty"); } catch (_) { record("parentDom", "blocked"); }
  try { record("appCookie", document.cookie || "none"); } catch (_) { record("appCookie", "blocked"); }
  try { global.top.location["ass" + "ign"]("/retrom-isolation-escape"); record("topNavigation", "exposed"); }
  catch (_) { record("topNavigation", "blocked"); }
  try { var popup = global["op" + "en"]("about:blank", "retrom-isolation-probe"); record("popup", popup ? "exposed" : "blocked"); if (popup) popup.close(); }
  catch (_) { record("popup", "blocked"); }
  try {
    var form = document.createElement("form"); form.method = "POST";
    form.action = ["https:", "//example.invalid/retrom-form"].join(""); form.target = "retrom-isolation-form";
    document.body.appendChild(form); form.submit(); record("form", "attempted");
  } catch (_) { record("form", "blocked"); }
  Promise.resolve().then(async function () {
    try { await global["fet" + "ch"](["https:", "//example.invalid/retrom-fetch"].join("")); record("externalFetch", "exposed"); }
    catch (_) { record("externalFetch", "blocked"); }
    try { var response = await global["fet" + "ch"]("/api/v1/health", { credentials: "include" }); record("nonAllowlistApi", response.status); }
    catch (_) { record("nonAllowlistApi", "blocked"); }
    try {
      var registration = await navigator.serviceWorker["reg" + "ister"]("/retrom-malicious-sw.js");
      record("serviceWorker", "exposed"); await registration.unregister();
    } catch (_) { record("serviceWorker", "blocked"); }
    results.complete = "true";
  });
})(window);
`

type matrixPlan struct {
	SchemaVersion int               `json:"schemaVersion"`
	WrongCore     []wrongCoreSource `json:"wrongCore"`
	Unsafe        []unsafeCase      `json:"unsafe"`
	Nested        []nestedCase      `json:"nestedArchives"`
}

type wrongCoreSource struct {
	Fixture    string            `json:"fixture"`
	Generation string            `json:"generation"`
	CoreID     string            `json:"coreId"`
	Targets    []wrongCoreTarget `json:"targets"`
}

type wrongCoreTarget struct {
	CoreID             string `json:"coreId"`
	ExpectedCode       string `json:"expectedCode,omitempty"`
	Accepted           bool   `json:"accepted"`
	EvidenceConfidence string `json:"evidenceConfidence,omitempty"`
}

type unsafeCase struct {
	Name         string `json:"name"`
	Fixture      string `json:"fixture"`
	SourceType   string `json:"sourceType"`
	CoreID       string `json:"coreId"`
	ExpectedCode string `json:"expectedCode"`
	Accepted     bool   `json:"accepted"`
}

type nestedCase struct {
	Generation string `json:"generation"`
	Fixture    string `json:"fixture"`
	CoreID     string `json:"coreId"`
	Sidecar    string `json:"sidecar"`
	Format     string `json:"format"`
	Detection  string `json:"detection"`
}

func generateSecurityFixtures(output string) error {
	if err := generateShapeProject(output, "malicious-rpgmv", "MV"); err != nil {
		return err
	}
	if err := generateShapeProject(output, "malicious-rpgmz", "MZ"); err != nil {
		return err
	}
	if err := generateNegativeInputs(output); err != nil {
		return err
	}
	return writeMatrixPlan(output)
}

func generateShapeProject(output, directory, engine string) error {
	prefix := directory + "/"
	core := "rpg_core.js"
	scripts := []string{"rpg_core.js", "rpg_managers.js", "rpg_objects.js", "rpg_scenes.js", "rpg_sprites.js", "rpg_windows.js"}
	version := "1.6.2"
	if engine == "MZ" {
		core = "rmmz_core.js"
		scripts = []string{"rmmz_core.js", "rmmz_managers.js", "rmmz_objects.js", "rmmz_scenes.js", "rmmz_sprites.js", "rmmz_windows.js"}
		version = "1.9.0"
	}
	scripts = append(scripts, "plugins.js", "main.js")
	var tags strings.Builder
	if engine == "MZ" {
		scripts = append([]string{"libs/localforage.min.js"}, scripts...)
	}
	for _, script := range scripts {
		tags.WriteString(`    <script src="js/` + script + `"></script>` + "\n")
		body := []byte("// Retrom-owned isolation marker\n")
		if script == core {
			body = []byte(`window.Utils = { RPGMAKER_NAME: "` + engine + `", RPGMAKER_VERSION: "` + version + `" };` + "\n" +
				`Utils.RPGMAKER_VERSION = "` + version + `";` + "\n")
		}
		if script == "main.js" {
			body = []byte(shapeRuntime + maliciousProbe)
		}
		if err := writeFile(output, prefix+"js/"+script, body); err != nil {
			return err
		}
	}
	index := "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>Retrom isolation harness</title></head>\n" +
		"<body><canvas width=\"640\" height=\"480\"></canvas>\n" + tags.String() + "</body></html>\n"
	if err := writeFile(output, prefix+"index.html", []byte(index)); err != nil {
		return err
	}
	system := []byte(`{"gameTitle":"RETROM ` + engine + ` ISOLATION HARNESS","startMapId":1,"startX":10,"startY":8}` + "\n")
	if err := writeFile(output, prefix+"data/System.json", system); err != nil {
		return err
	}
	if engine == "MZ" {
		for _, name := range []string{"Game.exe", "nw.dll", "plugin.node", "launcher.bat"} {
			if err := writeFile(output, prefix+name, []byte("RETROM OWNED OPAQUE NATIVE PAYLOAD: "+name+"\n")); err != nil {
				return err
			}
		}
	}
	return nil
}

func generateNegativeInputs(output string) error {
	inputs := map[string][]byte{
		"negative-matrix/dual-root/index.html":                  []byte("<script src=\"js/rpg_core.js\"></script>\n"),
		"negative-matrix/dual-root/data/System.json":            []byte("{}\n"),
		"negative-matrix/dual-root/js/rpg_core.js":              []byte("Utils.RPGMAKER_VERSION = \"1.6.2\";\n"),
		"negative-matrix/dual-root/www/index.html":              []byte("<script src=\"js/rpg_core.js\"></script>\n"),
		"negative-matrix/dual-root/www/data/System.json":        []byte("{}\n"),
		"negative-matrix/dual-root/www/js/rpg_core.js":          []byte("Utils.RPGMAKER_VERSION = \"1.6.2\";\n"),
		"negative-matrix/rgss-conflict/Game.ini":                []byte("[Game]\nScripts=Data/Scripts.rxdata\nLibrary=RGSS102E.dll\n"),
		"negative-matrix/rgss-conflict/Game.rxproj":             []byte("RPGXP 1.05\n"),
		"negative-matrix/rgss-conflict/Game.rvproj":             []byte("RPGVX 1.03\n"),
		"negative-matrix/lcf-truncated/RPG_RT.ldb":              []byte("\x0bLcfDataBase\x01"),
		"negative-matrix/lcf-truncated/RPG_RT.lmt":              []byte("\x0aLcfMapTree\x01"),
		"negative-matrix/case-collision/Game.ini":               []byte("[Game]\nScripts=Data/Scripts.rxdata\nLibrary=RGSS102E.dll\n"),
		"negative-matrix/case-collision/game.ini":               []byte("[Game]\nScripts=Data/Scripts.rvdata\nLibrary=RGSS202E.dll\n"),
		"negative-matrix/nfkc-collision/Game.ini":               []byte("[Game]\nScripts=Data/Scripts.rxdata\nLibrary=RGSS102E.dll\n"),
		"negative-matrix/nfkc-collision/Ｇame.ini":               []byte("[Game]\nScripts=Data/Scripts.rxdata\nLibrary=RGSS102E.dll\n"),
		"negative-matrix/external/index.html":                   []byte("<script src=\"https://example.invalid/payload.js\"></script>\n"),
		"negative-matrix/external/data/System.json":             []byte("{}\n"),
		"negative-matrix/external/js/rpg_core.js":               []byte("Utils.RPGMAKER_VERSION = \"1.6.2\";\n"),
		"negative-matrix/referenced-native/index.html":          []byte("<script src=\"plugin.node\"></script><script src=\"js/rpg_core.js\"></script>\n"),
		"negative-matrix/referenced-native/data/System.json":    []byte("{}\n"),
		"negative-matrix/referenced-native/js/rpg_core.js":      []byte("Utils.RPGMAKER_VERSION = \"1.6.2\";\n"),
		"negative-matrix/referenced-native/plugin.node":         []byte("RETROM OWNED REFERENCED NATIVE PAYLOAD\n"),
		"negative-matrix/gencache-collision/Picture/retrom.png": []byte("RETROM GENCACHE COLLISION PNG\n"),
		"negative-matrix/gencache-collision/Picture/retrom.jpg": []byte("RETROM GENCACHE COLLISION JPG\n"),
	}
	for _, project := range []string{"negative-matrix/external/", "negative-matrix/referenced-native/"} {
		for _, script := range []string{
			"rpg_managers.js", "rpg_objects.js", "rpg_scenes.js", "rpg_sprites.js", "rpg_windows.js",
			"plugins.js", "main.js",
		} {
			inputs[project+"js/"+script] = []byte("// RETROM OWNED MV MARKER\n")
		}
	}
	for name, body := range inputs {
		if err := writeFile(output, name, body); err != nil {
			return err
		}
	}
	archives := []struct {
		name    string
		entries []securityZIPEntry
	}{
		{name: "traversal.zip", entries: []securityZIPEntry{{name: "../index.html", body: []byte("escape")}}},
		{name: "symlink.zip", entries: []securityZIPEntry{{name: "index.html", body: []byte("target"), mode: fs.ModeSymlink | 0o777}}},
		{name: "bomb.zip", entries: []securityZIPEntry{{name: "payload.bin", body: bytes.Repeat([]byte("A"), 17<<20)}}},
	}
	for _, archive := range archives {
		body, err := deterministicSecurityZIP(archive.entries)
		if err != nil {
			return err
		}
		if err := writeFile(output, "negative-matrix/archives/"+archive.name, body); err != nil {
			return err
		}
	}
	return generateNestedSidecars(output)
}

func generateNestedSidecars(output string) error {
	inner := []securityZIPEntry{
		{name: "index.html", body: []byte("RETROM INNER FALSE PROJECT MARKER\n")},
		{name: "../escape.txt", body: []byte("RETROM INNER TRAVERSAL NAME\n")},
		{name: "compressed.bin", body: bytes.Repeat([]byte("Z"), 64<<10)},
	}
	zipBytes, err := deterministicSecurityZIP(inner)
	if err != nil {
		return err
	}
	tarBytes, err := deterministicSecurityTAR(inner)
	if err != nil {
		return err
	}
	var gzipBuffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&gzipBuffer)
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	if _, err := gzipWriter.Write(tarBytes); err != nil {
		return fmt.Errorf("write security gzip: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close security gzip: %w", err)
	}
	formats := []struct {
		name, extension string
		body            []byte
	}{
		{name: "zip", extension: ".zip", body: zipBytes},
		{name: "7z", extension: ".7z", body: append([]byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}, []byte("RETROM INNER FALSE PROJECT ../index.html COMPRESSED")...)},
		{name: "rar", extension: ".rar", body: []byte("Rar!RETROM INNER FALSE PROJECT ../index.html COMPRESSED")},
		{name: "tar", extension: ".tar", body: tarBytes},
		{name: "gzip", extension: ".gz", body: gzipBuffer.Bytes()},
	}
	for _, format := range formats {
		for _, suffix := range []string{format.extension, "-magic"} {
			if err := writeFile(output, "negative-matrix/sidecars/nested-"+format.name+suffix, format.body); err != nil {
				return err
			}
		}
	}
	return nil
}

func deterministicSecurityTAR(entries []securityZIPEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)),
			ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR,
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("create security TAR entry: %w", err)
		}
		if _, err := writer.Write(entry.body); err != nil {
			return nil, fmt.Errorf("write security TAR entry: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close security TAR: %w", err)
	}
	return output.Bytes(), nil
}

type securityZIPEntry struct {
	name string
	body []byte
	mode fs.FileMode
}

func deterministicSecurityZIP(entries []securityZIPEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		destination, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create security ZIP entry: %w", err)
		}
		if _, err := destination.Write(entry.body); err != nil {
			return nil, fmt.Errorf("write security ZIP entry: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close security ZIP: %w", err)
	}
	return output.Bytes(), nil
}

func writeMatrixPlan(output string) error {
	cores := []struct{ generation, fixture, core string }{
		{"RPG2000", "rpg2000", "rpgmaker_2000"}, {"RPG2003", "rpg2003", "rpgmaker_2003"},
		{"RPGXP", "rpgxp", "rpgmaker_xp"}, {"RPGVX", "rpgvx", "rpgmaker_vx"},
		{"RPGVXACE", "rpgvxace", "rpgmaker_vx_ace"}, {"RPGMV", "malicious-rpgmv", "rpgmaker_mv"},
		{"RPGMZ", "malicious-rpgmz", "rpgmaker_mz"},
	}
	plan := matrixPlan{SchemaVersion: 1}
	for _, source := range cores {
		targets := make([]wrongCoreTarget, 0, len(cores)-1)
		for _, target := range cores {
			if target.core != source.core {
				matrixTarget := wrongCoreTarget{CoreID: target.core, ExpectedCode: "RPG_SELECTED_CORE_MISMATCH"}
				if source.generation == "RPG2000" && target.core == "rpgmaker_2003" {
					matrixTarget = wrongCoreTarget{
						CoreID: target.core, Accepted: true, EvidenceConfidence: "FAMILY_ONLY",
					}
				}
				targets = append(targets, matrixTarget)
			}
		}
		plan.WrongCore = append(plan.WrongCore, wrongCoreSource{
			Fixture: source.fixture, Generation: source.generation, CoreID: source.core, Targets: targets,
		})
	}
	plan.Unsafe = []unsafeCase{
		{Name: "dual-root", Fixture: "negative-matrix/dual-root", SourceType: "DIRECTORY", CoreID: "rpgmaker_mv", ExpectedCode: "RPG_PROJECT_ROOT_AMBIGUOUS"},
		{Name: "multi-generation", Fixture: "malicious-rpgmv+malicious-rpgmz", SourceType: "COMPOSITE", CoreID: "rpgmaker_mv", ExpectedCode: "RPG_GENERATION_AMBIGUOUS"},
		{Name: "rgss-conflict", Fixture: "negative-matrix/rgss-conflict", SourceType: "DIRECTORY", CoreID: "rpgmaker_xp", ExpectedCode: "RPG_RGSS_GENERATION_CONFLICT"},
		{Name: "lcf-truncated", Fixture: "negative-matrix/lcf-truncated", SourceType: "DIRECTORY", CoreID: "rpgmaker_2000", ExpectedCode: "RPG_LCF_INVALID"},
		{Name: "case-collision", Fixture: "negative-matrix/case-collision", SourceType: "DIRECTORY", CoreID: "rpgmaker_xp", ExpectedCode: "RPG_PATH_COLLISION"},
		{Name: "nfkc-collision", Fixture: "negative-matrix/nfkc-collision", SourceType: "DIRECTORY", CoreID: "rpgmaker_xp", ExpectedCode: "RPG_PATH_COLLISION"},
		{Name: "gencache-collision", Fixture: "negative-matrix/gencache-collision", SourceType: "GENCACHE_COMPOSITE", CoreID: "rpgmaker_2000", ExpectedCode: "IMPORT_INPUT_INVALID"},
		{Name: "traversal", Fixture: "negative-matrix/archives/traversal.zip", SourceType: "FILES", CoreID: "rpgmaker_mv", ExpectedCode: "IMPORT_INPUT_INVALID"},
		{Name: "symlink", Fixture: "negative-matrix/archives/symlink.zip", SourceType: "FILES", CoreID: "rpgmaker_mv", ExpectedCode: "IMPORT_INPUT_INVALID"},
		{Name: "bomb", Fixture: "negative-matrix/archives/bomb.zip", SourceType: "FILES", CoreID: "rpgmaker_mv", ExpectedCode: "ARCHIVE_LIMIT_EXCEEDED"},
		{Name: "external", Fixture: "negative-matrix/external", SourceType: "DIRECTORY", CoreID: "rpgmaker_mv", ExpectedCode: "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"},
		{Name: "referenced-native", Fixture: "negative-matrix/referenced-native", SourceType: "DIRECTORY", CoreID: "rpgmaker_mv", ExpectedCode: "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"},
		{Name: "opaque-native", Fixture: "malicious-rpgmz", SourceType: "DIRECTORY", CoreID: "rpgmaker_mz", Accepted: true},
	}
	for _, source := range cores {
		for _, format := range []string{"zip", "7z", "rar", "tar", "gzip"} {
			for _, detection := range []string{"extension", "magic"} {
				suffix := "." + format
				if format == "gzip" {
					suffix = ".gz"
				}
				if detection == "magic" {
					suffix = "-magic"
				}
				plan.Nested = append(plan.Nested, nestedCase{
					Generation: source.generation, Fixture: source.fixture, CoreID: source.core,
					Sidecar: "negative-matrix/sidecars/nested-" + format + suffix,
					Format:  strings.ToUpper(format), Detection: detection,
				})
			}
		}
	}
	sort.Slice(plan.Nested, func(left, right int) bool {
		leftKey := plan.Nested[left].Generation + plan.Nested[left].Format + plan.Nested[left].Detection
		rightKey := plan.Nested[right].Generation + plan.Nested[right].Format + plan.Nested[right].Detection
		return leftKey < rightKey
	})
	contents, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode security matrix: %w", err)
	}
	return writeFile(output, "negative-matrix/matrix.json", append(contents, '\n'))
}
