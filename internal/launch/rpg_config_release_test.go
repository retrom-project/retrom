package launch

import (
	"testing"

	"retrom/internal/dependencies"
)

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
		set, "/runtime/rpgmaker/v0.2.0/", runtimeVersion,
		"mkxp-z_libretro.js", "mkxp-z_libretro.wasm", "c000000000000000000000000000000000000000000000000000000000000000",
	)
	if !ok || core.JSSizeBytes != 258192 || core.WasmSizeBytes != 42487229 ||
		core.JSSHA256[0] != 'a' || core.WasmSHA256[0] != 'b' ||
		core.JSURL != "/runtime/rpgmaker/v0.2.0/mkxp-z_libretro.js" ||
		core.WasmURL != "/runtime/rpgmaker/v0.2.0/mkxp-z_libretro.wasm" {
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
		set, "/runtime/rpgmaker/v0.2.0/", "v0.2.0",
		"mkxp-z_libretro.js", "mkxp-z_libretro.wasm", "c",
	); ok {
		t.Fatal("mkxp core config accepted mismatched release role")
	}
}
