package runtimecontract_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"retrom/internal/capability/runtime/runtimejson"

	"retrom/internal/capability/runtime/runtimebundle"
	"retrom/internal/capability/runtime/runtimecatalog"
	"retrom/internal/capability/runtime/runtimelaunch"
	"retrom/internal/capability/runtime/runtimeoptions"
	"retrom/internal/model/runtimecontract"
	runtimeprovidermodel "retrom/internal/model/runtimeprovider"
)

func digest(contents []byte) string {
	value := sha256.Sum256(contents)
	return hex.EncodeToString(value[:])
}

func errFacts(err error) map[string]any {
	text, kind := "", ""
	if err != nil {
		text = err.Error()
		kind = fmt.Sprintf("%T", err)
	}
	return map[string]any{"text": text, "type": kind, "manifest": errors.Is(err, runtimecontract.ErrManifestInvalid), "bareManifest": reflect.TypeOf(err) == reflect.TypeOf(runtimecontract.ErrManifestInvalid) && errors.Is(err, runtimecontract.ErrManifestInvalid), "launch": errors.Is(err, runtimebundle.ErrLaunchEnvelopeInvalid), "envelope": errors.Is(err, runtimelaunch.ErrEnvelopeInvalid), "optionsInvalid": errors.Is(err, runtimeoptions.ErrInvalid), "optionsUnsupported": errors.Is(err, runtimeoptions.ErrUnsupported), "active": errors.Is(err, runtimebundle.ErrActiveInvalid), "integrity": errors.Is(err, runtimebundle.ErrIntegrityInvalid), "projection": errors.Is(err, runtimeprovidermodel.ErrProjectionInvalid)}
}

func encoded(value any) map[string]any {
	contents, err := json.Marshal(value)
	return map[string]any{"json": string(contents), "sha256": digest(contents), "error": errFacts(err)}
}

func numericTypes(value any, path string, result map[string]string) {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			numericTypes(item, path+"."+k, result)
		}
	case runtimecontract.TargetOptionsSchema:
		for k, item := range v {
			parsed, err := runtimejson.ParseStrictJSON(item)
			if err != nil {
				panic(err)
			}
			numericTypes(parsed, path+"."+k, result)
		}
	case []any:
		for i, item := range v {
			numericTypes(item, fmt.Sprintf("%s[%d]", path, i), result)
		}
	case int, int64, float64, json.Number:
		result[path] = reflect.TypeOf(value).String()
	}
}

func (observer *providerObserver) decodeSchema(name string, contents []byte) {
	for _, mode := range []string{"direct", "encoding-json"} {
		var schema runtimecontract.TargetOptionsSchema
		if err := schema.UnmarshalJSON([]byte(simpleSchema)); err != nil {
			panic(err)
		}
		before := providerRaw(schema)
		var err error
		if mode == "direct" {
			err = schema.UnmarshalJSON(contents)
		} else {
			err = json.Unmarshal(contents, &schema)
		}
		after := providerRaw(schema)
		numbers := map[string]string{}
		numericTypes(schema, "schema", numbers)
		observer.schemaCases[name+"/"+mode] = map[string]any{"inputHex": hex.EncodeToString(contents), "error": errFacts(err), "before": string(before), "after": string(after), "receiverUnchanged": string(before) == string(after), "numbers": numbers}
	}
}

func nilReceiver(contents []byte) map[string]any {
	result := map[string]any{}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result["panic"] = fmt.Sprint(recovered)
				result["panicType"] = fmt.Sprintf("%T", recovered)
			}
		}()
		var schema *runtimecontract.TargetOptionsSchema
		result["error"] = errFacts(schema.UnmarshalJSON(contents))
	}()
	return result
}

func numberSchema(value string) string {
	return `{"type":"object","additionalProperties":false,"properties":{"n":{"type":"integer","minimum":` + value + `}},"required":["n"]}`
}
func strptr(s string) *string { return &s }
func fill(b string) string    { return strings.Repeat(b, 64) }
func fixture() (*runtimelaunch.Builder, runtimecontract.Binding, runtimecontract.ActiveDescriptor, runtimecontract.Manifest) {
	checkpoint := &runtimecontract.Checkpoint{WriteFormat: "fixture-state-v1", ReadFormats: []string{"fixture-state-v1"}, MaxBytes: 1024}
	var schema runtimecontract.TargetOptionsSchema
	if err := schema.UnmarshalJSON([]byte(simpleSchema)); err != nil {
		panic(err)
	}
	target := runtimecontract.Target{
		ID: "target", DisplayName: "Fixture", TargetOptionsSchema: schema,
		Inputs:       []runtimecontract.Input{{Role: "game", Kind: "ROM_BLOB", Cardinality: "ONE"}},
		Capabilities: runtimecontract.Capabilities{Checkpoint: true, NetplayPort: true, FrameMode: "NONE", VideoModes: []string{}},
		Checkpoint:   checkpoint, AssetPaths: []string{"client.mjs"},
	}
	manifest := runtimecontract.Manifest{SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: "1.0.0", ProviderAPI: 1, ClientModulePath: "client.mjs", Targets: []runtimecontract.Target{target}}
	active := runtimecontract.ActiveDescriptor{SchemaVersion: 1, Source: "candidate", SourceTreeSHA256: strptr(fill("9")), Providers: []runtimecontract.ActiveProvider{{
		ProviderID: "fixture", ProviderVersion: "1.0.0", ProviderAPI: 1, BundleSHA256: fill("a"), ModuleSHA256: fill("b"), ManifestSHA256: fill("c"), ClientModulePath: "client.mjs",
		InstallationPath: "fixture/" + fill("a"), BundleSizeBytes: 1, FileCount: 3, UnpackedSizeBytes: 3,
		Targets: []runtimecontract.ActiveTarget{{ID: "target", Checkpoint: checkpoint}},
	}}}
	builder, err := runtimelaunch.NewBuilder(active, map[string]runtimecontract.Manifest{"fixture": manifest})
	if err != nil {
		panic(err)
	}
	return builder, runtimecontract.Binding{ProviderID: "fixture", TargetID: "target", LaunchPolicy: "SUPPORTED"}, active, manifest
}

func baseInput(binding runtimecontract.Binding) runtimecontract.LaunchInput {
	return runtimecontract.LaunchInput{
		Binding: binding, Session: runtimecontract.LaunchSession{
			ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
			Title: "Fixture <&> 游戏", PlatformName: "Fixture", CoreName: "Fixture Core", ReturnTo: "/games/fixture",
		}, Resources: providerResourceJSON([]map[string]any{{"kind": "ROM_BLOB", "ordinal": 0, "rangeRequired": false, "role": "game", "sha256": fill("e"), "sizeBytes": 3, "url": "/runtime/content/game"}}),
		TargetOptions: providerRaw(map[string]any{}),
	}
}

func (observer *providerObserver) buildCase(name string, builder *runtimelaunch.Builder, input runtimecontract.LaunchInput) {
	contents, err := builder.Build(input)
	parseErr := error(nil)
	if err == nil {
		_, parseErr = runtimebundle.ParseLaunchEnvelope(contents)
	}
	observer.buildCases[name] = map[string]any{"input": encoded(input), "json": string(contents), "sha256": digest(contents), "error": errFacts(err), "parsedError": errFacts(parseErr)}
}

const (
	simpleSchema   = `{"type":"object","additionalProperties":false,"properties":{},"required":[]}`
	emulatorSchema = `{"type":"object","additionalProperties":false,"properties":{"dosEntryPath":{"type":["string","null"]},"initialDiscIndex":{"type":["integer","null"],"minimum":0}},"required":["dosEntryPath","initialDiscIndex"]}`
)

type providerObserver struct {
	values, schemaCases, buildCases, fixtureCases, results map[string]any
}

func TestProviderValuesMatchPreMigrationGo(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("testdata/provider-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]json.RawMessage
	if err := json.Unmarshal(contents, &expected); err != nil {
		t.Fatal(err)
	}
	observer := &providerObserver{
		values: map[string]any{}, schemaCases: map[string]any{}, buildCases: map[string]any{},
		fixtureCases: map[string]any{}, results: map[string]any{},
	}
	observer.observeValues()
	observer.observeSchemas()
	observer.observeParsers()
	observer.observeBuilds()
	observer.observeOptions()
	observer.observeProjection()
	observer.observePublicFixtures()
	observer.results["values"] = observer.values
	observer.results["schemaCases"] = observer.schemaCases
	observer.results["buildCases"] = observer.buildCases
	observer.results["publicLaunchFixtures"] = observer.fixtureCases
	for name, actual := range observer.results {
		t.Run(name, func(t *testing.T) {
			actualJSON, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			var expectedValue any
			if err := json.Unmarshal(expected[name], &expectedValue); err != nil {
				t.Fatal(err)
			}
			expectedJSON, err := json.Marshal(expectedValue)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actualJSON, expectedJSON) {
				compareProviderObservations(t, expectedValue, actual)
			}
		})
	}
}

func compareProviderObservations(t *testing.T, expected, actual any) {
	t.Helper()
	expectedMap, expectedOK := expected.(map[string]any)
	actualMap, actualOK := actual.(map[string]any)
	if expectedOK && actualOK {
		if len(expectedMap) != len(actualMap) {
			t.Fatalf("observation count = %d, want %d", len(actualMap), len(expectedMap))
		}
		for key, value := range expectedMap {
			actualValue, exists := actualMap[key]
			if !exists {
				t.Errorf("missing observation %q", key)
				continue
			}
			t.Run(key, func(t *testing.T) { compareProviderObservations(t, value, actualValue) })
		}
		return
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actualJSON, expectedJSON) {
		t.Errorf("pre-migration observation changed:\nwant %s\n got %s", expectedJSON, actualJSON)
	}
}

// providerRaw uses the same encoding/json operation as the old assembly exit.
func providerRaw(value any) json.RawMessage {
	contents, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return contents
}

func providerResourceJSON(resources []map[string]any) []json.RawMessage {
	result := make([]json.RawMessage, len(resources))
	for index, resource := range resources {
		result[index] = providerRaw(resource)
	}
	return result
}

func providerMutateRaw(contents json.RawMessage, key string, value any) json.RawMessage {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(contents, &object); err != nil {
		panic(err)
	}
	object[key] = providerRaw(value)
	return providerRaw(object)
}

func (observer *providerObserver) observeValues() {
	_, _, active, manifest := fixture()

	for name, value := range map[string]any{
		"zero/manifest": runtimecontract.Manifest{}, "zero/target": runtimecontract.Target{}, "zero/input": runtimecontract.Input{},
		"zero/capabilities": runtimecontract.Capabilities{}, "zero/checkpoint": runtimecontract.Checkpoint{},
		"zero/integrity-file": runtimecontract.IntegrityFile{}, "zero/integrity": runtimecontract.Integrity{},
		"zero/active-descriptor": runtimecontract.ActiveDescriptor{}, "zero/release-identity": runtimecontract.ReleaseIdentity{},
		"zero/active-provider": runtimecontract.ActiveProvider{}, "zero/active-target": runtimecontract.ActiveTarget{},
		"zero/target-options-schema":  runtimecontract.TargetOptionsSchema(nil),
		"empty/target-options-schema": runtimecontract.TargetOptionsSchema{},
		"zero/launch-session":         runtimecontract.LaunchSession{}, "zero/launch-input": runtimecontract.LaunchInput{},
		"empty/launch-input":                       runtimecontract.LaunchInput{Session: runtimecontract.LaunchSession{Warnings: []string{}}, Resources: providerResourceJSON([]map[string]any{}), TargetOptions: providerRaw(map[string]any{}), Restore: providerRaw(map[string]any{}), Netplay: providerRaw(map[string]any{})},
		"integrity-file/media-omitted":             runtimecontract.IntegrityFile{Path: "file", SizeBytes: 7, SHA256: fill("c"), MediaType: "application/wasm"},
		"schema/programmatic-key-order":            runtimecontract.TargetOptionsSchema{"\ue000": providerRaw(int64(1)), "\U00010000": providerRaw(int64(2))},
		"schema/programmatic-fraction":             runtimecontract.TargetOptionsSchema{"number": providerRaw(1.5)},
		"schema/programmatic-bigint":               runtimecontract.TargetOptionsSchema{"number": providerRaw(int64(9007199254740992))},
		"schema/programmatic-json-number-exponent": runtimecontract.TargetOptionsSchema{"number": providerRaw(json.Number("1e0"))},
	} {
		observer.values[name] = encoded(value)
	}
	for name, value := range map[string]any{
		"populated/manifest": manifest, "populated/target": manifest.Targets[0], "populated/active": active,
		"checkpoint/empty-semantics": manifest.Targets[0].Checkpoint, "release/sample": runtimecontract.ReleaseIdentity{Repository: "https://github.com/retrom-project/retrom-runtime", Tag: "v1.0.0", Commit: strings.Repeat("a", 40)},
		"integrity/sample": runtimecontract.Integrity{SchemaVersion: 1, Files: []runtimecontract.IntegrityFile{{Path: "client.mjs", SizeBytes: 7, SHA256: fill("c"), MediaType: "text/javascript; charset=utf-8"}}},
	} {
		observer.values[name] = encoded(value)
	}
	for _, semantics := range []string{"INSTANT", "GAME_SAVE"} {
		cp := *manifest.Targets[0].Checkpoint
		cp.Semantics = semantics
		observer.values["checkpoint/"+semantics] = encoded(cp)
	}
}

func (observer *providerObserver) observeSchemas() {
	observer.decodeSchema("empty-object-schema", []byte(simpleSchema))
	for _, item := range []struct{ name, value string }{
		{"max-safe", "9007199254740991"},
		{"min-safe", "-9007199254740991"},
		{"unsafe-positive", "9007199254740992"},
		{"unsafe-negative", "-9007199254740992"},
		{"fraction", "1.5"},
		{"decimal-whole", "1.0"},
		{"exponent", "1e0"},
		{"leading-zero", "01"},
		{"negative-zero", "-0"},
	} {
		observer.decodeSchema(item.name, []byte(numberSchema(item.value)))
	}
	unicodeSchema := `{"type":"object","additionalProperties":false,"properties":{"choice":{"type":"string","enum":["<&>","\u2028","💾"]}},"required":["choice"]}`
	observer.decodeSchema("unicode-and-escaping", []byte(unicodeSchema))
	for name, input := range map[string]string{
		"null": "null", "empty-map": "{}", "root-array": "[]",
		"duplicate-root":   strings.Replace(simpleSchema, `"type":"object"`, `"type":"object","type":"object"`, 1),
		"duplicate-nested": strings.Replace(numberSchema("1"), `"minimum":1`, `"minimum":1,"minimum":2`, 1),
		"trailing":         simpleSchema + " {}", "unknown-key": strings.Replace(simpleSchema, `"type":"object"`, `"type":"object","unknown":true`, 1),
		"high-surrogate": strings.Replace(unicodeSchema, "💾", `\ud800`, 1),
		"low-surrogate":  strings.Replace(unicodeSchema, "💾", `\udc00`, 1),
		"surrogate-pair": strings.Replace(unicodeSchema, "💾", `\ud83d\udcbe`, 1),
	} {
		observer.decodeSchema(name, []byte(input))
	}
	malformed := []byte(strings.Replace(unicodeSchema, "💾", string([]byte{0xff}), 1))
	observer.decodeSchema("invalid-utf8", malformed)
	observer.results["nilReceiverValid"] = nilReceiver([]byte(simpleSchema))
	observer.results["nilReceiverInvalid"] = nilReceiver([]byte("{}"))
}

func (observer *providerObserver) observeParsers() {
	_, _, active, _ := fixture()
	parserCases := map[string]any{}
	parsedManifest, manifestErr := runtimebundle.ParseManifest([]byte(rawFixtureManifest))
	if manifestErr != nil {
		panic(manifestErr)
	}
	observer.values["parsed/manifest"] = encoded(parsedManifest)
	bound, boundErr := runtimebundle.BindTargetIntegrity(parsedManifest, []runtimecontract.IntegrityFile{{Path: "assets/core.wasm", SizeBytes: 1, SHA256: fill("a")}})
	if boundErr != nil {
		panic(boundErr)
	}
	observer.values["parsed/bound-manifest"] = encoded(bound)
	for name, contents := range map[string]string{
		"valid":            rawFixtureManifest,
		"unknown":          strings.Replace(rawFixtureManifest, `"schemaVersion":1`, `"schemaVersion":1,"adapterId":"leaked"`, 1),
		"missing-volume":   strings.Replace(rawFixtureManifest, `"volume":true,`, "", 1),
		"provider-api-two": strings.Replace(rawFixtureManifest, `"providerApiVersion":1`, `"providerApiVersion":2`, 1),
	} {
		value, e := runtimebundle.ParseManifest([]byte(contents))
		parserCases["manifest/"+name] = map[string]any{"source": contents, "value": encoded(value), "error": errFacts(e)}
	}
	activeJSON, e := json.Marshal(active)
	if e != nil {
		panic(e)
	}
	for name, contents := range map[string][]byte{"valid": activeJSON, "provider-api-two": []byte(strings.Replace(string(activeJSON), `"providerApiVersion":1`, `"providerApiVersion":2`, 1))} {
		value, e := runtimebundle.ParseActiveDescriptor(contents)
		parserCases["active/"+name] = map[string]any{"source": string(contents), "value": encoded(value), "error": errFacts(e)}
	}
	integrityJSON := `{"schemaVersion":1,"files":[{"path":"assets/core.wasm","sizeBytes":4,"sha256":"` + fill("a") + `","mediaType":"application/wasm"},{"path":"client.mjs","sizeBytes":8,"sha256":"` + fill("b") + `","mediaType":"text/javascript; charset=utf-8"},{"path":"provider.json","sizeBytes":2,"sha256":"` + fill("c") + `","mediaType":"application/json; charset=utf-8"}]}`
	for name, contents := range map[string]string{"valid": integrityJSON, "wrong-media": strings.Replace(integrityJSON, "application/wasm", "video/mp4", 1), "path-escape": strings.Replace(integrityJSON, "client.mjs", "../client.mjs", 1)} {
		value, e := runtimebundle.ParseIntegrity([]byte(contents))
		parserCases["integrity/"+name] = map[string]any{"source": contents, "value": encoded(value), "error": errFacts(e)}
	}
	observer.results["parserCases"] = parserCases
}

func (observer *providerObserver) observeBuilds() {
	builder, binding, _, _ := fixture()

	observer.buildCase("single-nil-warnings", builder, baseInput(binding))
	emptyWarnings := baseInput(binding)
	emptyWarnings.Session.Warnings = []string{}
	observer.buildCase("single-empty-warnings", builder, emptyWarnings)
	observer.buildCase("nil-builder", nil, baseInput(binding))
	withRestore := baseInput(binding)
	withRestore.Restore = providerRaw(map[string]any{"format": "fixture-state-v1", "sha256": fill("f"), "sizeBytes": 3, "url": "/runtime/checkpoints/fixture"})
	observer.buildCase("restore", builder, withRestore)
	withNetplay := baseInput(binding)
	withNetplay.Session.Mode = "NETPLAY"
	withNetplay.Netplay = providerRaw(map[string]any{"roomId": "fixture-room", "sessionId": "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", "playerNo": int64(1), "socketUrl": "wss://retrom.example.test/socket", "profile": map[string]any{"fps": int64(60), "description": "<&> 游戏"}})
	observer.buildCase("netplay", builder, withNetplay)
	for _, name := range []string{"unknown-target", "disabled", "missing-resource", "resource-kind", "resource-shape", "nil-options", "undeclared-options", "restore-invalid", "netplay-mode", "malformed-restore-raw"} {
		input := baseInput(binding)
		switch name {
		case "unknown-target":
			input.Binding.TargetID = "unknown"
		case "disabled":
			input.Binding.LaunchPolicy = "DISABLED"
		case "missing-resource":
			input.Resources = nil
		case "resource-kind":
			input.Resources[0] = providerMutateRaw(input.Resources[0], "kind", "FILE_TREE")
		case "resource-shape":
			input.Resources[0] = providerMutateRaw(input.Resources[0], "sizeBytes", int64(-1))
		case "nil-options":
			input.TargetOptions = nil
		case "undeclared-options":
			input.TargetOptions = providerMutateRaw(input.TargetOptions, "unknown", true)
		case "restore-invalid":
			input.Restore = providerRaw(map[string]any{})
		case "netplay-mode":
			input.Session.Mode = "NETPLAY"
		case "malformed-restore-raw":
			input.Restore = json.RawMessage("{")
		}
		observer.buildCase(name, builder, input)
	}
}

func (observer *providerObserver) observeOptions() {
	var es runtimecontract.TargetOptionsSchema
	if err := es.UnmarshalJSON([]byte(emulatorSchema)); err != nil {
		panic(err)
	}
	optionCases := map[string]any{}
	for _, test := range []struct {
		name, id string
		input    runtimeoptions.Input
	}{
		{"emulator-single", runtimecatalog.OptionsEmulator, runtimeoptions.Input{ContentKind: "SINGLE_FILE", InitialDiscIndex: 9}},
		{"emulator-dos", runtimecatalog.OptionsEmulator, runtimeoptions.Input{ContentKind: "DOS_BUNDLE", DOSEntry: strptr("GAME.EXE")}},
		{"emulator-disc", runtimecatalog.OptionsEmulator, runtimeoptions.Input{ContentKind: "MULTI_DISC", InitialDiscIndex: 2}},
		{"emulator-invalid-disc", runtimecatalog.OptionsEmulator, runtimeoptions.Input{ContentKind: "MULTI_DISC", InitialDiscIndex: -1}},
		{"unknown-strategy", "UNKNOWN", runtimeoptions.Input{}},
	} {
		value, err := runtimeoptions.Build(test.id, es, test.input)
		optionCases[test.name] = map[string]any{"value": encoded(value), "error": errFacts(err)}
	}
}

func (observer *providerObserver) observeProjection() {
	_, _, active, manifest := fixture()
	var es runtimecontract.TargetOptionsSchema
	if err := es.UnmarshalJSON([]byte(emulatorSchema)); err != nil {
		panic(err)
	}

	manifest.Targets[0].TargetOptionsSchema = es
	catalog := runtimecontract.Catalog{SchemaVersion: 1, Definitions: runtimecontract.Definitions{
		Platforms:    []runtimecontract.PlatformDefinition{{ID: "gbc", Name: "Game Boy / Color", SortOrder: 40, Enabled: true}},
		Cores:        []runtimecontract.CoreDefinition{{ID: "gambatte", Name: "Gambatte", Enabled: true}},
		ContentKinds: []string{"SINGLE_FILE"}, AssetPacks: []runtimecontract.AssetPackDefinition{},
	}, Bindings: []runtimecontract.Binding{{ID: "fixture-target", CoreID: "gambatte", ProviderID: "fixture", TargetID: "target", PlatformIDs: []string{"gbc"}, AcceptedContentKinds: []string{"SINGLE_FILE"}, DetectorProfile: "EMULATORJS_SINGLE_FILE", LaunchPolicy: "SUPPORTED"}}}
	projection, err := runtimeprovidermodel.NewProjection(active, map[string]runtimecontract.Manifest{"fixture": manifest}, catalog)
	if err != nil {
		panic(err)
	}
	observer.values["projection/complete"] = encoded(projection)
}

func (observer *providerObserver) observePublicFixtures() {
	for _, dir := range []string{"valid", "invalid"} {
		entries, err := os.ReadDir(filepath.Join("..", "..", "..", "api/runtime-provider/v1/fixtures", dir))
		if err != nil {
			panic(err)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join("..", "..", "..", "api/runtime-provider/v1/fixtures", dir, entry.Name())
			contents, err := os.ReadFile(path)
			if err != nil {
				panic(err)
			}
			parsed, parseErr := runtimebundle.ParseLaunchEnvelope(contents)
			observer.fixtureCases[dir+"/"+entry.Name()] = map[string]any{"sourceSHA256": digest(contents), "parsed": encoded(parsed), "error": errFacts(parseErr)}
		}
	}
}

var rawFixtureManifest = "{\n  \"schemaVersion\":1,\n  \"providerId\":\"fixture\",\n  \"providerVersion\":\"1.0.0\",\n  " +
	"\"providerApiVersion\":1,\n  \"clientModulePath\":\"client.mjs\",\n  \"targets\":[{\n    \"i" +
	"d\":\"core\",\n    \"displayName\":\"Core\",\n    \"targetOptionsSchema\":{\"type\":\"object\"," +
	"\"additionalProperties\":false,\"properties\":{},\"required\":[]},\n    \"inputs\":[{\"rol" +
	"e\":\"game\",\"kind\":\"ROM_BLOB\",\"cardinality\":\"ONE\",\"optional\":false}],\n    \"capabil" +
	"ities\":{\"pause\":true,\"screenshot\":true,\"checkpoint\":false,\"standardGamepad\":true" +
	",\"frameCounter\":false,\"volume\":true,\"discSwitch\":false,\"nativeSettings\":true,\"in" +
	"putFilter\":true,\"netplayPort\":false,\"videoModes\":[\"original\",\"pixel\"],\"requiresT" +
	"hreads\":false,\"frameMode\":\"SAME_ORIGIN_BLANK\"},\n    \"checkpoint\":null,\n    \"asse" +
	"tPaths\":[\"assets/core.wasm\"]\n  }]\n}"
