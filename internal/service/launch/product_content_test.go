package launch

import (
	"errors"
	"testing"

	"retrom/internal/capability/content/corevalidation"
)

func TestProductContentSelectsDeclaredDeliveryFiles(t *testing.T) {
	for _, test := range []struct {
		name, delivery, kind, role, file, format string
		variant, blocked                         bool
	}{
		{"single", "ROM_BLOB", "SINGLE_FILE", "CONTENT", "game.bin", "SOURCE_V1", false, false},
		{"DOS bundle", "EMULATORJS_CONTENT", "DOS_BUNDLE", "DOS_LAUNCH_BUNDLE", "game.zip", "RETROM_DOS_DIRECT_ZIP_V1", true, false},
		{"DOS source cannot launch", "EMULATORJS_CONTENT", "DOS_BUNDLE", "DOS_SOURCE", "game.zip", "", false, true},
		{"DOS bundle name", "EMULATORJS_CONTENT", "DOS_BUNDLE", "DOS_LAUNCH_BUNDLE", "wrong.zip", "", true, true},
		{"native project", "FILE_TREE_PROJECT", "NXENGINE_PROJECT_V1", "PROJECT_FILE", "data/game.bin", "NXENGINE_PROJECT_V1", false, false},
		{"isolated project", "ISOLATED_WEB_PROJECT", "TYRANOSCRIPT_PROJECT_V1", "PROJECT_FILE", "index.html", "TYRANOSCRIPT_PROJECT_V1", false, false},
		{"missing content", "ROM_BLOB", "SINGLE_FILE", "PROJECT_FILE", "game.bin", "", false, true},
		{"unsupported delivery", "UNKNOWN", "SINGLE_FILE", "CONTENT", "game.bin", "", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := ProductSnapshot{Source: ProductSource{DeliveryProfile: test.delivery, ContentKind: test.kind}}
			file := ProductFile{Role: test.role, BlobID: "blob", LogicalName: test.file}
			if test.variant {
				snapshot.VariantFiles = []ProductFile{file}
			} else {
				snapshot.GameFiles = []ProductFile{file}
			}
			content, err := BuildProductContent(snapshot)
			if test.blocked {
				if !errors.Is(err, ErrBlocked) || len(content.Files) != 0 {
					t.Fatalf("files=%+v error=%v", content.Files, err)
				}
			} else if err != nil || len(content.Files) != 1 || content.Files[0].Format != test.format || content.Files[0].BlobID != "blob" {
				t.Fatalf("files=%+v error=%v", content.Files, err)
			}
		})
	}
}

func TestProductExternalBIOSRetainsOptionalAndCollisionPolicy(t *testing.T) {
	status, blob, virtual := "MATCHED", "bios", "/bios.bin"
	for _, test := range []struct {
		name, logical, mode              string
		present, allowedMissing, blocked bool
	}{
		{"required available", "bios.bin", "REQUIRED", true, false, false},
		{"required absent", "bios.bin", "REQUIRED", false, false, true},
		{"optional absent", "bios.bin", "OPTIONAL", false, false, false},
		{"shared preview omission", "bios.bin", "REQUIRED", false, true, false},
		{"content name collision", "GAME.BIN", "REQUIRED", true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dependency := corevalidation.BIOSDependency{BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{LogicalName: test.logical, RequirementMode: test.mode, DeliveryKind: "EXTERNAL_FILE", EmulatorPath: &virtual}}
			if test.present {
				dependency.BlobID, dependency.InstallationStatus = &blob, &status
			}
			files, err := productExternalBIOS("game.bin", nil, []corevalidation.BIOSDependency{dependency}, test.allowedMissing)
			if errors.Is(err, ErrBlocked) != test.blocked || (test.present && !test.blocked) != (len(files) == 1) {
				t.Fatalf("files=%+v error=%v", files, err)
			}
		})
	}
}
