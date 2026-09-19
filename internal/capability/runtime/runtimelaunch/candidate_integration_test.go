package runtimelaunch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/capability/runtime/runtimebundle"
	"retrom/internal/capability/runtime/runtimecatalog"
	runtimecontract "retrom/internal/model/runtimecontract"
)

const expectedCandidateBindingMatrix = `
emulatorjs-81 81 emulatorjs 81 EMULATORJS_SINGLE_FILE
emulatorjs-a5200 a5200 emulatorjs a5200 EMULATORJS_SINGLE_FILE
emulatorjs-azahar azahar emulatorjs azahar EMULATORJS_SINGLE_FILE
emulatorjs-beetle-vb beetle_vb emulatorjs beetle-vb EMULATORJS_SINGLE_FILE
emulatorjs-bsnes bsnes emulatorjs bsnes EMULATORJS_SINGLE_FILE
emulatorjs-cap32 cap32 emulatorjs cap32 EMULATORJS_SINGLE_FILE
emulatorjs-crocods crocods emulatorjs crocods EMULATORJS_SINGLE_FILE
emulatorjs-desmume desmume emulatorjs desmume EMULATORJS_SINGLE_FILE
emulatorjs-desmume2015 desmume2015 emulatorjs desmume2015 EMULATORJS_SINGLE_FILE
emulatorjs-dosbox-pure dosbox_pure emulatorjs dosbox-pure DOS_BUNDLE
emulatorjs-fbalpha2012-cps1 fbalpha2012_cps1 emulatorjs fbalpha2012-cps1 ARCADE_ROM_SET
emulatorjs-fbalpha2012-cps2 fbalpha2012_cps2 emulatorjs fbalpha2012-cps2 ARCADE_ROM_SET
emulatorjs-fbneo fbneo emulatorjs fbneo ARCADE_ROM_SET
emulatorjs-fceumm fceumm emulatorjs fceumm EMULATORJS_SINGLE_FILE
emulatorjs-flycast flycast emulatorjs flycast EMULATORJS_SINGLE_FILE
emulatorjs-freeintv freeintv emulatorjs freeintv EMULATORJS_SINGLE_FILE
emulatorjs-fuse fuse emulatorjs fuse EMULATORJS_SINGLE_FILE
emulatorjs-gam4980 gam4980 emulatorjs gam4980 EMULATORJS_SINGLE_FILE
emulatorjs-gambatte gambatte emulatorjs gambatte EMULATORJS_SINGLE_FILE
emulatorjs-gearcoleco gearcoleco emulatorjs gearcoleco EMULATORJS_SINGLE_FILE
emulatorjs-genesis-plus-gx genesis_plus_gx emulatorjs genesis-plus-gx EMULATORJS_SINGLE_FILE
emulatorjs-genesis-plus-gx-wide genesis_plus_gx_wide emulatorjs genesis-plus-gx-wide EMULATORJS_SINGLE_FILE
emulatorjs-handy handy emulatorjs handy EMULATORJS_SINGLE_FILE
emulatorjs-mame2003 mame2003 emulatorjs mame2003 ARCADE_ROM_SET
emulatorjs-mame2003-plus mame2003_plus emulatorjs mame2003-plus ARCADE_ROM_SET
emulatorjs-mednafen-ngp mednafen_ngp emulatorjs mednafen-ngp EMULATORJS_SINGLE_FILE
emulatorjs-mednafen-pce mednafen_pce emulatorjs mednafen-pce EMULATORJS_SINGLE_FILE
emulatorjs-mednafen-pcfx mednafen_pcfx emulatorjs mednafen-pcfx EMULATORJS_SINGLE_FILE
emulatorjs-mednafen-psx-hw mednafen_psx_hw emulatorjs mednafen-psx-hw EMULATORJS_SINGLE_FILE
emulatorjs-mednafen-wswan mednafen_wswan emulatorjs mednafen-wswan EMULATORJS_SINGLE_FILE
emulatorjs-melonds melonds emulatorjs melonds EMULATORJS_SINGLE_FILE
emulatorjs-mgba mgba emulatorjs mgba EMULATORJS_SINGLE_FILE
emulatorjs-mupen64plus-next mupen64plus_next emulatorjs mupen64plus-next EMULATORJS_SINGLE_FILE
emulatorjs-neocd neocd emulatorjs neocd EMULATORJS_SINGLE_FILE
emulatorjs-nestopia nestopia emulatorjs nestopia EMULATORJS_SINGLE_FILE
emulatorjs-opera opera emulatorjs opera EMULATORJS_SINGLE_FILE
emulatorjs-parallel-n64 parallel_n64 emulatorjs parallel-n64 EMULATORJS_SINGLE_FILE
emulatorjs-pcsx-rearmed pcsx_rearmed emulatorjs pcsx-rearmed EMULATORJS_SINGLE_FILE
emulatorjs-picodrive picodrive emulatorjs picodrive EMULATORJS_SINGLE_FILE
emulatorjs-prboom prboom emulatorjs prboom EMULATORJS_SINGLE_FILE
emulatorjs-prosystem prosystem emulatorjs prosystem EMULATORJS_SINGLE_FILE
emulatorjs-puae puae emulatorjs puae EMULATORJS_SINGLE_FILE
emulatorjs-quasi88 quasi88 emulatorjs quasi88 EMULATORJS_SINGLE_FILE
emulatorjs-same-cdi same_cdi emulatorjs same-cdi EMULATORJS_SINGLE_FILE
emulatorjs-smsplus smsplus emulatorjs smsplus EMULATORJS_SINGLE_FILE
emulatorjs-snes9x snes9x emulatorjs snes9x EMULATORJS_SINGLE_FILE
emulatorjs-stella2014 stella2014 emulatorjs stella2014 EMULATORJS_SINGLE_FILE
emulatorjs-uzem uzem emulatorjs uzem EMULATORJS_SINGLE_FILE
emulatorjs-vecx vecx emulatorjs vecx EMULATORJS_SINGLE_FILE
emulatorjs-vice-x128 vice_x128 emulatorjs vice-x128 EMULATORJS_SINGLE_FILE
emulatorjs-vice-x64 vice_x64 emulatorjs vice-x64 EMULATORJS_SINGLE_FILE
emulatorjs-vice-x64sc vice_x64sc emulatorjs vice-x64sc EMULATORJS_SINGLE_FILE
emulatorjs-vice-xpet vice_xpet emulatorjs vice-xpet EMULATORJS_SINGLE_FILE
emulatorjs-vice-xplus4 vice_xplus4 emulatorjs vice-xplus4 EMULATORJS_SINGLE_FILE
emulatorjs-vice-xvic vice_xvic emulatorjs vice-xvic EMULATORJS_SINGLE_FILE
emulatorjs-virtualjaguar virtualjaguar emulatorjs virtualjaguar EMULATORJS_SINGLE_FILE
emulatorjs-yabause yabause emulatorjs yabause EMULATORJS_DISC_CONTENT
retrom-runtime-butterscotch-gamemaker butterscotch retrom-runtime butterscotch-gamemaker BUTTERSCOTCH_PROJECT
retrom-runtime-fake08 fake08 retrom-runtime fake08 PICO8_CART
retrom-runtime-flash-ruffle ruffle retrom-runtime flash-ruffle FLASH_SWF
retrom-runtime-gbe-pokemini gbe_plus retrom-runtime gbe-pokemini POKEMINI_ROM
retrom-runtime-j2me j2me retrom-runtime j2me J2ME_JAR
retrom-runtime-kirikiri2-kag kirikiri2 retrom-runtime kirikiri2-kag KIRIKIRI_PROJECT
retrom-runtime-msx-webmsx webmsx retrom-runtime msx-webmsx MSX_MEDIA
retrom-runtime-np2kai np2kai retrom-runtime np2kai-pc98 PC98_DISK
retrom-runtime-nxengine nxengine retrom-runtime nxengine NXENGINE_PROJECT
retrom-runtime-onscripter-yuri onscripter_yuri retrom-runtime onscripter-yuri ONS_PROJECT
retrom-runtime-openbor openbor retrom-runtime openbor OPENBOR_PAK
retrom-runtime-play play retrom-runtime play-ps2 OPTICAL_DISC
retrom-runtime-ppsspp ppsspp retrom-runtime ppsspp OPTICAL_DISC
retrom-runtime-px68k px68k retrom-runtime px68k PX68K_DISK
retrom-runtime-rpgmaker-2000 rpgmaker retrom-runtime rpgmaker-2000 RPG2000
retrom-runtime-rpgmaker-2003 rpgmaker retrom-runtime rpgmaker-2003 RPG2003
retrom-runtime-rpgmaker-mv rpgmaker retrom-runtime rpgmaker-mv RPGMV
retrom-runtime-rpgmaker-mz rpgmaker retrom-runtime rpgmaker-mz RPGMZ
retrom-runtime-rpgmaker-vx rpgmaker retrom-runtime rpgmaker-vx RPGVX
retrom-runtime-rpgmaker-vx-ace rpgmaker retrom-runtime rpgmaker-vx-ace RPGVXACE
retrom-runtime-rpgmaker-xp rpgmaker retrom-runtime rpgmaker-xp RPGXP
retrom-runtime-scummvm scummvm retrom-runtime scummvm SCUMMVM_PROJECT
retrom-runtime-tic80 tic80 retrom-runtime tic80 TIC80_CART
retrom-runtime-tyranoscript tyranoscript retrom-runtime tyranoscript TYRANOSCRIPT_PROJECT
retrom-runtime-wasm4 wasm4 retrom-runtime wasm4 WASM4_CART
`

func TestInstalledCandidateBuildsDeterministicEnvelopeForEveryCatalogBinding(t *testing.T) {
	root := os.Getenv("RETROM_PROVIDER_TEST_ROOT")
	if root == "" {
		t.Skip("RETROM_PROVIDER_TEST_ROOT is required for the cross-repository candidate matrix")
	}
	activeContents, err := os.ReadFile(filepath.Join(root, "active.json"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := runtimebundle.ParseActiveDescriptor(activeContents)
	if err != nil {
		t.Fatal(err)
	}
	manifests := loadCandidateManifests(t, root, active)
	builder, err := NewBuilder(active, manifests)
	if err != nil {
		t.Fatal(err)
	}
	catalogContents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(catalogContents)
	if err != nil {
		t.Fatal(err)
	}
	assertCandidateBindingMatrix(t, catalog.Bindings)

	for _, binding := range catalog.Bindings {
		t.Run(binding.ProviderID+"/"+binding.TargetID, func(t *testing.T) {
			assertCandidateBinding(t, builder, manifests, binding)
		})
	}
}

func assertCandidateBindingMatrix(t *testing.T, bindings []runtimecontract.Binding) {
	t.Helper()
	const columns = 5
	fields := strings.Fields(expectedCandidateBindingMatrix)
	if len(fields)%columns != 0 {
		t.Fatalf("expected binding matrix has %d trailing fields", len(fields)%columns)
	}
	if len(bindings) != len(fields)/columns {
		t.Fatalf("binding count = %d, want %d", len(bindings), len(fields)/columns)
	}
	for index, binding := range bindings {
		offset := index * columns
		expected := strings.Join(fields[offset:offset+columns], "\x00")
		actual := strings.Join([]string{
			binding.ID, binding.CoreID, binding.ProviderID, binding.TargetID, binding.DetectorProfile,
		}, "\x00")
		if actual != expected {
			t.Fatalf("binding[%d] = %q, want %q", index, actual, expected)
		}
	}
}

func loadCandidateManifests(
	t *testing.T,
	root string,
	active runtimebundle.ActiveDescriptor,
) map[string]runtimebundle.Manifest {
	t.Helper()
	manifests := make(map[string]runtimebundle.Manifest, len(active.Providers))
	for _, provider := range active.Providers {
		directory := filepath.Join(root, "installed", filepath.FromSlash(provider.InstallationPath))
		manifestContents, readErr := os.ReadFile(filepath.Join(directory, "provider.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		manifest, parseErr := runtimebundle.ParseManifest(manifestContents)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		integrityContents, readErr := os.ReadFile(filepath.Join(directory, "integrity.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		integrity, parseErr := runtimebundle.ParseIntegrity(integrityContents)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		bound, bindErr := runtimebundle.BindTargetIntegrity(manifest, integrity.Files)
		if bindErr != nil {
			t.Fatal(bindErr)
		}
		manifests[provider.ProviderID] = bound
	}
	return manifests
}

func assertCandidateBinding(
	t *testing.T,
	builder *Builder,
	manifests map[string]runtimebundle.Manifest,
	binding runtimecontract.Binding,
) {
	t.Helper()
	target := findManifestTarget(t, manifests[binding.ProviderID], binding.TargetID)
	input := Input{Binding: binding, Session: Session{
		ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
		Title: target.DisplayName, PlatformName: binding.PlatformIDs[0], CoreName: binding.CoreID,
		ReturnTo: "/games/fixture",
	}, Resources: resourcesForTarget(target), TargetOptions: optionsForBinding(t, binding, target.TargetOptionsSchema)}
	first, err := builder.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := builder.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("Envelope bytes are not deterministic")
	}
	var envelope map[string]any
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	assertForbiddenKeysAbsent(t, envelope)
}

func findManifestTarget(t *testing.T, manifest runtimebundle.Manifest, targetID string) runtimebundle.Target {
	t.Helper()
	for _, target := range manifest.Targets {
		if target.ID == targetID {
			return target
		}
	}
	t.Fatalf("target %q missing", targetID)
	return runtimebundle.Target{}
}

func resourcesForTarget(target runtimebundle.Target) []map[string]any {
	result := make([]map[string]any, 0, len(target.Inputs))
	for _, input := range target.Inputs {
		base := map[string]any{"kind": input.Kind, "ordinal": 0, "role": input.Role}
		switch input.Kind {
		case "ROM_BLOB", "SEEKABLE_BLOB", "PARENT_ARCHIVE", "WASM4_CART":
			base["rangeRequired"] = input.Kind == "SEEKABLE_BLOB" || input.Kind == "PARENT_ARCHIVE"
			base["sha256"], base["sizeBytes"], base["url"] = strings.Repeat("e", 64), 3, "/runtime/content/"+input.Role
		case "FILE_TREE":
			base["contentDigest"], base["indexUrl"] = strings.Repeat("e", 64), "/runtime/content/"+input.Role+"/index"
		case "NATIVE_WEB", "ISOLATED_WEB":
			base["bootstrapTicket"], base["cleanupUrl"] = strings.Repeat("t", 48), "https://runtime.example.test/cleanup"
			base["contentDigest"], base["entryUrl"] = strings.Repeat("e", 64), "https://runtime.example.test/entry"
			base["origin"] = "https://runtime.example.test"
		case "BIOS_BUNDLE", "EXTERNAL_FILE_SET":
			base["files"] = []map[string]any{{
				"logicalName": "firmware", "sha256": strings.Repeat("e", 64),
				"sizeBytes": 3, "url": "/runtime/content/" + input.Role + "/firmware", "virtualPath": "firmware.bin",
			}}
		case "MULTI_DISC":
			base["entries"] = []map[string]any{{
				"index": 0, "label": "Disc 1", "sha256": strings.Repeat("e", 64),
				"sizeBytes": 3, "url": "/runtime/content/" + input.Role + "/disc-1",
			}}
			base["initialDiscIndex"] = 0
		default:
			panic(fmt.Sprintf("unhandled resource kind %q", input.Kind))
		}
		result = append(result, base)
	}
	return result
}

func optionsForBinding(
	t *testing.T,
	binding runtimecontract.Binding,
	schema runtimebundle.TargetOptionsSchema,
) map[string]any {
	t.Helper()
	strategy, ok := runtimecatalog.Strategy(binding.DetectorProfile)
	if !ok {
		t.Fatalf("binding %q has no Host strategy", binding.ID)
	}
	var result map[string]any
	switch strategy.Options {
	case runtimecatalog.OptionsNone:
		result = map[string]any{}
	case runtimecatalog.OptionsEmulator:
		result = map[string]any{"dosEntryPath": nil, "initialDiscIndex": nil}
	case runtimecatalog.OptionsONS:
		result = map[string]any{"scriptEncoding": "utf8"}
	case runtimecatalog.OptionsKiriKiri:
		result = map[string]any{"startupXp3Path": nil}
	case runtimecatalog.OptionsScummVM:
		result = map[string]any{
			"engineId": "sky", "gameId": "sky", "root": "Game", "language": "en", "platform": "pc",
			"extra": "Floppy", "guiOptions": "", "filename": nil,
		}
	default:
		t.Fatalf("binding %q uses unhandled option strategy %q", binding.ID, strategy.Options)
	}
	if !runtimebundle.ValidateTargetOptions(schema, result) {
		t.Fatalf("binding %q fixture does not satisfy target options schema: %#v", binding.ID, result)
	}
	return result
}

func assertForbiddenKeysAbsent(t *testing.T, value any) {
	t.Helper()
	forbidden := map[string]bool{
		"routeKey": true, "runtimeFamily": true, "adapterKind": true, "adapterAbi": true,
		"saveAbi": true, "payloadKind": true, "nativeProfile": true, "resumeSlot": true, "coreArtifactId": true,
	}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if forbidden[key] {
					t.Fatalf("forbidden key %q in Envelope", key)
				}
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(value)
}
