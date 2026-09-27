package detector

import (
	"strings"
	"testing"
)

func TestDetectMVAndMZExactMarkers(t *testing.T) {
	mv, err := Detect("rpgmaker_mv", mvProject())
	if err != nil {
		t.Fatalf("Detect(MV) error = %v", err)
	}
	assertProfile(t, mv, RPGMV, RPGMV)
	if mv.EngineVersion != "1.6.2" || mv.EvidenceFamily != FamilyMV {
		t.Fatalf("MV engine version = %q", mv.EngineVersion)
	}

	mz, err := Detect("rpgmaker_mz", mzProject())
	if err != nil {
		t.Fatalf("Detect(MZ) error = %v", err)
	}
	assertProfile(t, mz, RPGMZ, RPGMZ)
	if mz.EngineVersion != "1.9.0" || mz.EvidenceFamily != FamilyMZ {
		t.Fatalf("MZ engine version = %q", mz.EngineVersion)
	}
	alternate := mzProject()
	delete(alternate, "js/libs/localforage.min.js")
	alternate["js/libs/localforage.js"] = []byte("// localforage fixture")
	alternate["index.html"] = []byte(strings.ReplaceAll(
		string(alternate["index.html"]), "js/libs/localforage.min.js", "js/libs/localforage.js",
	))
	if _, err := Detect("rpgmaker_mz", alternate); err != nil {
		t.Fatalf("Detect(MZ alternate localforage) error = %v", err)
	}
}

func TestWebDetectorRejectsStrictJSONFailures(t *testing.T) {
	tests := [][]byte{
		[]byte(`{"gameTitle":"one","gameTitle":"two"}`),
		[]byte(`{"nested":{"value":1,"value":2}}`),
		[]byte(`{"value":1} trailing`),
		[]byte(`["not an object"]`),
		[]byte(`{"tooLarge":1e9999}`),
	}
	for index, contents := range tests {
		t.Run(string(rune('A'+index)), func(t *testing.T) {
			project := mvProject()
			project["data/System.json"] = contents
			_, err := Detect("rpgmaker_mv", project)
			assertErrorCode(t, err, CodeWebFormatInvalid)
		})
	}
}

func TestWebDetectorRejectsUnsafeBrowserDependencies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(memoryIndex)
		code   Code
	}{
		{name: "external script", mutate: func(project memoryIndex) {
			project["index.html"] = []byte(`<script src="https://example.invalid/a.js"></script>`)
		}, code: CodeNativeDependencyUnsupported},
		{name: "missing local resource", mutate: func(project memoryIndex) { project["index.html"] = []byte(`<script src="js/missing.js"></script>`) }, code: CodeWebFormatInvalid},
		{name: "external base", mutate: func(project memoryIndex) {
			project["index.html"] = []byte(`<base href="//cdn.invalid/"><script src="js/main.js"></script>`)
		}, code: CodeNativeDependencyUnsupported},
		{name: "form action", mutate: func(project memoryIndex) { project["index.html"] = []byte(`<form action="/upload"></form>`) }, code: CodeNativeDependencyUnsupported},
		{name: "top navigation", mutate: func(project memoryIndex) {
			project["index.html"] = []byte(`<a href="safe.html" target="_top">leave</a>`)
			project["safe.html"] = []byte("safe")
		}, code: CodeNativeDependencyUnsupported},
		{name: "inline script", mutate: func(project memoryIndex) { project["index.html"] = []byte(`<script>window.example = true</script>`) }, code: CodeNativeBridgeUnsupported},
		{name: "popup", mutate: func(project memoryIndex) { project["js/main.js"] = []byte(`window.open("help.html")`) }, code: CodeNativeDependencyUnsupported},
		{name: "external fetch", mutate: func(project memoryIndex) { project["js/main.js"] = []byte(`fetch("https://example.invalid/state")`) }, code: CodeNativeDependencyUnsupported},
		{name: "external xhr", mutate: func(project memoryIndex) {
			project["js/main.js"] = []byte(`request.open("GET", "//example.invalid/state")`)
		}, code: CodeNativeDependencyUnsupported},
		{name: "referenced native module", mutate: func(project memoryIndex) {
			project["plugin.node"] = []byte("opaque native payload")
			project["index.html"] = []byte(`<script src="plugin.node"></script>`)
		}, code: CodeNativeDependencyUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := mvProject()
			test.mutate(project)
			_, err := Detect("rpgmaker_mv", project)
			assertErrorCode(t, err, test.code)
		})
	}
}

func TestWebDetectorRetainsOpaqueDesktopFiles(t *testing.T) {
	t.Parallel()
	project := mzProject()
	for _, name := range []string{
		"Game.exe", "nw.dll", "plugin.node", "launcher.bat", "helper.cmd", "setup.ps1",
		"libgame.so", "libgame.dylib",
	} {
		project[name] = []byte("opaque desktop payload")
	}
	profile, err := Detect("rpgmaker_mz", project)
	if err != nil {
		t.Fatalf("Detect(MZ with opaque desktop files) error = %v", err)
	}
	assertProfile(t, profile, RPGMZ, RPGMZ)
}

func TestWebDetectorAllowsDesktopCompatibilityBranches(t *testing.T) {
	tests := []string{
		`if (Utils.isNwjs()) { require("fs").writeFileSync("save/file1.rpgsave", data); }`,
		`if (typeof nw !== "undefined") { nw.Window.get(); }`,
		`process.nextTick = function(callback) { setTimeout(callback, 0); };`,
		`this._goldWindow.open(); this.window.open();`,
		`const source = "this._mapNameWindow.open();};//----------------";`,
	}
	for _, source := range tests {
		project := mvProject()
		project["js/main.js"] = []byte(source)
		if _, err := Detect("rpgmaker_mv", project); err != nil {
			t.Errorf("Detect(MV desktop compatibility branch) error = %v", err)
		}
	}
}

func TestWebDetectorRejectsBothCompleteMarkerSets(t *testing.T) {
	project := mvProject()
	for name, contents := range mzProject() {
		if _, present := project[name]; !present {
			project[name] = contents
		}
	}
	_, err := Detect("rpgmaker_mv", project)
	assertErrorCode(t, err, CodeGenerationAmbiguous)
}

func mvProject() memoryIndex {
	return webProject([]string{
		"js/rpg_core.js", "js/rpg_managers.js", "js/rpg_objects.js", "js/rpg_scenes.js",
		"js/rpg_sprites.js", "js/rpg_windows.js", "js/plugins.js", "js/main.js",
	}, "js/rpg_core.js", `Utils.RPGMAKER_VERSION = "1.6.2";`)
}

func mzProject() memoryIndex {
	return webProject([]string{
		"js/rmmz_core.js", "js/rmmz_managers.js", "js/rmmz_objects.js", "js/rmmz_scenes.js",
		"js/rmmz_sprites.js", "js/rmmz_windows.js", "js/plugins.js", "js/main.js",
		"js/libs/localforage.min.js",
	}, "js/rmmz_core.js", `Utils.RPGMAKER_VERSION = "1.9.0";`)
}

func webProject(scripts []string, versionFile, versionAssignment string) memoryIndex {
	project := memoryIndex{"data/System.json": []byte(`{"gameTitle":"Retrom Fixture"}`)}
	html := "<!doctype html><html><head>"
	for _, script := range scripts {
		project[script] = []byte("// fixture")
		html += `<script src="` + script + `"></script>`
	}
	project[versionFile] = []byte(versionAssignment)
	project["index.html"] = []byte(html + "</head><body></body></html>")
	return project
}
