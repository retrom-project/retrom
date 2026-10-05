package runtimeprovider

import (
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/content/requirements"
	runtimebundle "retrom/internal/runtime/bundle"
)

func TestDATPairRejectsDifferentShippedCoreOrDAT(t *testing.T) {
	expected := runtimebundle.ArcadeDAT{
		Format: "ARCADE_XML",
		Core:   requirements.Asset{Path: "assets/core.data", SHA256: strings.Repeat("a", 64)},
		Asset:  requirements.Asset{Path: "assets/game.dat", SHA256: strings.Repeat("b", 64)},
	}
	pair := datPair{SchemaVersion: 1, Kind: "CORE_DAT_PAIR", SourceCommit: strings.Repeat("c", 40), BuildConfigSHA256: strings.Repeat("d", 64)}
	pair.Core.Filename, pair.Core.SHA256 = "core.data", expected.Core.SHA256
	pair.DAT.Filename, pair.DAT.SHA256 = "game.dat", expected.Asset.SHA256
	pair.Generator.Method, pair.Generator.Symbol = "SHIPPED_WASM", "retrom_export_arcade_dat"
	pair.Generator.WasmSHA256, pair.Generator.SHA256 = strings.Repeat("e", 64), strings.Repeat("f", 64)
	for _, change := range []string{"matched", "core", "dat", "timestamp"} {
		t.Run(change, func(t *testing.T) {
			value := pair
			switch change {
			case "core":
				value.Core.SHA256 = strings.Repeat("0", 64)
			case "dat":
				value.DAT.SHA256 = strings.Repeat("0", 64)
			case "timestamp":
				value.Generator.Method = "BUILD_TIMESTAMP"
			}
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if validDATPair(contents, expected) != (change == "matched") {
				t.Fatal("pair verification did not bind shipped artifacts")
			}
		})
	}
}
