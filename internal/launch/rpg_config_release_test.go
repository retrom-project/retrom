package launch

import (
	"encoding/json"
	"testing"

	"retrom/internal/dependencies"
)

func TestRPGSeekableBlobSourceIsStrictAndComplete(t *testing.T) {
	t.Parallel()

	source, ok := newRPGSeekableBlobSource(
		"/runtime/projects/0198abcd-1234-7123-8abc-1234567890ab/__retrom__/game.mkxpz",
		"a000000000000000000000000000000000000000000000000000000000000000",
		42,
		"",
	)
	encoded, err := json.Marshal(source)
	if !ok || err != nil || string(encoded) !=
		`{"kind":"SEEKABLE_BLOB_V1","rangeRequired":true,"url":"/runtime/projects/0198abcd-1234-7123-8abc-1234567890ab/__retrom__/game.mkxpz","sha256":"a000000000000000000000000000000000000000000000000000000000000000","sizeBytes":42}` {
		t.Fatalf("seekable source = %s, available=%v, error=%v", encoded, ok, err)
	}
	if _, valid := newRPGSeekableBlobSource("/runtime/project", "bad", 42, ""); valid {
		t.Fatal("seekable source accepted an invalid digest")
	}
	if _, valid := newRPGSeekableBlobSource(
		"/runtime/project",
		"a000000000000000000000000000000000000000000000000000000000000000",
		0,
		"",
	); valid {
		t.Fatal("seekable source accepted an empty payload")
	}
}

func TestMKXPCoreConfigUsesObservedReleaseCoordinates(t *testing.T) {
	t.Parallel()

	const runtimeVersion = "v0.2.0"
	set := &dependencies.Set{RPGMaker: &dependencies.RPGMakerVersion{
		Allowlist: map[string]dependencies.RPGMakerRuntimeFile{
			runtimeVersion + "/mkxp-z_libretro.js": {
				Path: runtimeVersion + "/mkxp-z_libretro.js", Role: "runtime_js",
				SizeBytes: 258192, SHA256: "a000000000000000000000000000000000000000000000000000000000000000",
			},
			runtimeVersion + "/mkxp-z_libretro.wasm": {
				Path: runtimeVersion + "/mkxp-z_libretro.wasm", Role: "runtime_wasm",
				SizeBytes: 42487229, SHA256: "b000000000000000000000000000000000000000000000000000000000000000",
			},
		},
	}}

	core, ok := mkxpCoreConfig(
		set, "/runtime/retrom-runtime/v0.2.0/", runtimeVersion,
		"mkxp-z_libretro.js", "mkxp-z_libretro.wasm", "c000000000000000000000000000000000000000000000000000000000000000",
	)
	if !ok || core.JSSizeBytes != 258192 || core.WasmSizeBytes != 42487229 ||
		core.JSSHA256[0] != 'a' || core.WasmSHA256[0] != 'b' ||
		core.JSURL != "/runtime/retrom-runtime/v0.2.0/mkxp-z_libretro.js" ||
		core.WasmURL != "/runtime/retrom-runtime/v0.2.0/mkxp-z_libretro.wasm" {
		t.Fatalf("mkxp core config = %#v, available=%v", core, ok)
	}
}

func TestMKXPCoreConfigRejectsWrongObservedRole(t *testing.T) {
	t.Parallel()

	set := &dependencies.Set{RPGMaker: &dependencies.RPGMakerVersion{
		Allowlist: map[string]dependencies.RPGMakerRuntimeFile{
			"v0.2.0/mkxp-z_libretro.js": {
				Path: "v0.2.0/mkxp-z_libretro.js", Role: "runtime_wasm", SizeBytes: 1, SHA256: "a",
			},
			"v0.2.0/mkxp-z_libretro.wasm": {
				Path: "v0.2.0/mkxp-z_libretro.wasm", Role: "runtime_wasm", SizeBytes: 1, SHA256: "b",
			},
		},
	}}
	if _, ok := mkxpCoreConfig(
		set, "/runtime/retrom-runtime/v0.2.0/", "v0.2.0",
		"mkxp-z_libretro.js", "mkxp-z_libretro.wasm", "c",
	); ok {
		t.Fatal("mkxp core config accepted mismatched release role")
	}
}
