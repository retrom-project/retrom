package dependencies

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type staticBIOS struct {
	coreID       string
	logical      string
	mode         string
	condition    string
	size         int64
	md5          string
	sha256       string
	options      string
	sourceURL    string
	delivery     string
	emulatorPath string
}

var staticBIOSCatalog = []staticBIOS{
	{
		coreID:    "fceumm",
		logical:   "disksys.rom",
		mode:      "CONDITIONAL",
		condition: "FDS_CONTENT",
		size:      8192,
		md5:       "ca30b50f880eb660a320674ed365ef7a",
		sha256:    "99c18490ed9002d9c6d999b9d8d15be5c051bdfa7cc7e73318053c9a994b0178",
		sourceURL: "https://docs.libretro.com/library/fceumm/",
	},
	{
		coreID:    "fceumm",
		logical:   "gamegenie.nes",
		mode:      "CONDITIONAL",
		condition: "GAME_GENIE_ADDON_MODE",
		md5:       "7f98d77d7a094ad7d069b74bd553ec98",
		sourceURL: "https://docs.libretro.com/library/fceumm/",
	},
	{
		coreID:    "snes9x",
		logical:   "BS-X.bin",
		mode:      "OPTIONAL",
		condition: "SNES_BSX_FIRMWARE",
		size:      1048576,
		md5:       "fed4d8242cfbed61343d53d48432aced",
		sha256:    "3ce321496edc5d77038de2034eb3fb354d7724afd0bc7fd0319f3eb5d57b984d",
		sourceURL: "https://docs.libretro.com/library/snes9x/",
	},
	{
		coreID:    "snes9x",
		logical:   "STBIOS.bin",
		mode:      "OPTIONAL",
		condition: "SNES_SUFAMI_FIRMWARE",
		size:      262144,
		md5:       "d3a44ba7d42a74d3ac58cb9c14c6a5ca",
		sha256:    "edacb453da14f825f05d1134d6035f4bf034e55f7cfb97c70c4ee107eabc7342",
		sourceURL: "https://docs.libretro.com/library/snes9x/",
	},
	{
		coreID:    "gambatte",
		logical:   "gb_bios.bin",
		mode:      "OPTIONAL",
		condition: "GB_CONTENT",
		size:      256,
		md5:       "32fbbd84168d3482956eb3c5051637f5",
		sha256:    "cf053eccb4ccafff9e67339d4e78e98dce7d1ed59be819d2a1ba2232c6fce1c7",
		options:   `{"gambatte_gb_bootloader":"enabled"}`,
		sourceURL: "https://docs.libretro.com/library/gambatte/",
	},
	{
		coreID:    "gambatte",
		logical:   "gbc_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBC_CONTENT",
		size:      2304,
		md5:       "dbfce9db9deaa2567f6a84fde55f9680",
		sha256:    "b4f2e416a35eef52cba161b159c7c8523a92594facb924b3ede0d722867c50c7",
		options:   `{"gambatte_gb_bootloader":"enabled"}`,
		sourceURL: "https://docs.libretro.com/library/gambatte/",
	},
	{
		coreID:    "mgba",
		logical:   "gba_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBA_CONTENT",
		size:      16384,
		md5:       "a860e8c0b6d573d191e4ec7db1b1e4f6",
		sha256:    "fd2547724b505f487e6dcb29ec2ecff3af35a841a77ab2e85fd87350abd36570",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "gb_bios.bin",
		mode:      "OPTIONAL",
		condition: "GB_CONTENT",
		size:      256,
		md5:       "32fbbd84168d3482956eb3c5051637f5",
		sha256:    "cf053eccb4ccafff9e67339d4e78e98dce7d1ed59be819d2a1ba2232c6fce1c7",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "gbc_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBC_CONTENT",
		size:      2304,
		md5:       "dbfce9db9deaa2567f6a84fde55f9680",
		sha256:    "b4f2e416a35eef52cba161b159c7c8523a92594facb924b3ede0d722867c50c7",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "sgb_bios.bin",
		mode:      "OPTIONAL",
		condition: "MGBA_SGB_MODEL",
		size:      256,
		md5:       "d574d4f9c12f305074798f54c091a8b4",
		sha256:    "0e4ddff32fc9d1eeaae812a157dd246459b00c9e14f2f61751f661f32361e360",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID: "nestopia", logical: "disksys.rom", mode: "CONDITIONAL", condition: "FDS_CONTENT", size: 8192,
		md5: "ca30b50f880eb660a320674ed365ef7a", sha256: "99c18490ed9002d9c6d999b9d8d15be5c051bdfa7cc7e73318053c9a994b0178",
		sourceURL: "https://docs.libretro.com/library/nestopia_ue/",
	},
	{
		coreID: "melonds", logical: "bios7.bin", mode: "REQUIRED", size: 16384,
		md5: "df692a80a5b1bc90728bc3dfc76cd948", sha256: "ba65f690eb04ec92db67c0e299e21ad71de087d6d5de8a9cb17a62eaab563c17",
		sourceURL:    "https://docs.libretro.com/library/melonds/",
		delivery:     "EXTERNAL_FILE",
		emulatorPath: "/retroarch/userdata/system/bios7.bin",
	},
	{
		coreID: "melonds", logical: "bios9.bin", mode: "REQUIRED", size: 4096,
		md5: "a392174eb3e572fed6447e956bde4b25", sha256: "1693983a7707ae394786fa526c0552457888a51d4e410d715ef07acd5a540555",
		sourceURL:    "https://docs.libretro.com/library/melonds/",
		delivery:     "EXTERNAL_FILE",
		emulatorPath: "/retroarch/userdata/system/bios9.bin",
	},
	{
		coreID: "melonds", logical: "firmware.bin", mode: "REQUIRED", size: 262144,
		md5: "6de7f8d5bdf66f6f5583fac51fcc5a07", sha256: "7d0e3e7f9ae2d9eda596d889ed8ce6d517da227460c120c0ab8d54432246380d",
		sourceURL:    "https://docs.libretro.com/library/melonds/",
		delivery:     "EXTERNAL_FILE",
		emulatorPath: "/retroarch/userdata/system/firmware.bin",
	},
	{
		coreID: "a5200", logical: "5200.rom", mode: "REQUIRED", size: 2048,
		md5: "281f20ea4320404ec820fb7ec0693b38", sha256: "06b250f18983d058c0f156ce7ee88ae48b6eaf11e6f10f21dccf6ac7ffb6a6af",
		sourceURL: "https://docs.libretro.com/library/atari800/",
	},
	{
		coreID: "pcsx_rearmed", logical: "scph5500.bin", mode: "REQUIRED", size: 524288,
		md5: "8dd7d5296a650fac7319bce665a6a53c", sha256: "9c0421858e217805f4abe18698afea8d5aa36ff0727eb8484944e00eb5e7eadb",
		sourceURL: "https://docs.libretro.com/library/pcsx_rearmed/",
	},
	{
		coreID: "mednafen_psx_hw", logical: "scph5500.bin", mode: "REQUIRED", size: 524288,
		md5: "8dd7d5296a650fac7319bce665a6a53c", sha256: "9c0421858e217805f4abe18698afea8d5aa36ff0727eb8484944e00eb5e7eadb",
		sourceURL: "https://docs.libretro.com/library/beetle_psx_hw/",
	},
	{
		coreID: "handy", logical: "lynxboot.img", mode: "REQUIRED", size: 512,
		md5: "fcd403db69f54290b51035d82f835e7b", sha256: "c26a36c1990bcf841155e5a6fea4d2ee1a4d53b3cc772e70f257a962ad43b383",
		sourceURL: "https://docs.libretro.com/library/handy/",
	},
	{
		coreID: "yabause", logical: "saturn_bios.bin", mode: "REQUIRED", size: 524288,
		md5: "af5828fdff51384f99b3c4926be27762", sha256: "ae4058627bb5db9be6d8d83c6be95a4aa981acc8a89042e517e73317886c8bc2",
		sourceURL: "https://docs.libretro.com/library/yabause/",
	},
	{
		coreID: "opera", logical: "panafz10.bin", mode: "REQUIRED", size: 1048576,
		md5: "51f2f43ae2f3508a14d9f56597e2d3ce", sha256: "8d72334395cfc98e44c89804eabf036cf95a23645353e7fe8ab886445a3b6354",
		sourceURL: "https://docs.libretro.com/library/opera/",
	},
	{
		coreID: "prosystem", logical: "7800 BIOS (U).rom", mode: "REQUIRED", size: 4096,
		md5: "0763f1ffb006ddbe32e52d497ee848ae", sha256: "7d94551defcd8e7b045a34255654d6d169a683f63062d51dee3eedabf2042db0",
		sourceURL: "https://docs.libretro.com/library/prosystem/",
	},
	{
		coreID: "mednafen_pcfx", logical: "pcfx.rom", mode: "REQUIRED", size: 1048576,
		md5: "08e36edbea28a017f79f8d4f7ff9b6d7", sha256: "4b44ccf5d84cc83daa2e6a2bee00fdafa14eb58bdf5859e96d8861a891675417",
		sourceURL: "https://docs.libretro.com/library/beetle_pc_fx/",
	},
}

// Static BIOS definitions are synchronized atomically with their aliases and version provenance.
func bootstrapStaticBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	selectedTargets map[string]runtimeTarget,
	now time.Time,
) error {
	if err := validateBIOSActivationOptions(staticBIOSCatalog); err != nil {
		return err
	}
	for _, requirement := range staticBIOSCatalog {
		target, selected := selectedTargets[requirement.coreID]
		if !selected {
			continue
		}
		delivery := requirement.delivery
		if delivery == "" {
			delivery = "BIOS_BUNDLE"
		}
		canonical, _ := json.Marshal(
			map[string]any{
				"activationOptions": json.RawMessage(nullableJSON(requirement.options)),
				"conditionCode":     requirement.condition,
				"deliveryKind":      delivery,
				"emulatorPath":      nullableStringValue(requirement.emulatorPath),
				"logicalName":       requirement.logical,
				"md5":               requirement.md5,
				"mode":              requirement.mode,
				"sha256":            nullableStringValue(requirement.sha256),
				"sizeBytes":         nullablePositive(requirement.size),
			},
		)
		digest := sha256.Sum256(canonical)
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(
			"retrom:bios:"+target.providerID+":"+target.targetID+":"+requirement.logical,
		)).String()
		_, err := transaction.ExecContext(
			ctx,
			`
INSERT INTO bios_requirements(id,
core_id,
provider_id,
target_id,
source_kind,
dat_machine_name,
logical_name,
requirement_mode,
condition_code,
activation_options_json,
catalog_digest,
size_bytes,
md5,
sha1,
sha256,
source_url,
source_version,
enabled,
version,
created_at_ms,
updated_at_ms,
delivery_kind,
emulator_path) VALUES(?,
?,
?,
?,
'STATIC',
NULL,
?,
?,
?,
?,
?,
?,
?,
NULL,
?,
?,
?,
1,
1,
?,
?,
?,
?) ON CONFLICT(provider_id,target_id,
logical_name)
DO UPDATE SET requirement_mode=excluded.requirement_mode,
condition_code=excluded.condition_code,
activation_options_json=excluded.activation_options_json,
catalog_digest=excluded.catalog_digest,
size_bytes=excluded.size_bytes,
md5=excluded.md5,
sha256=excluded.sha256,
source_url=excluded.source_url,
source_version=excluded.source_version,
delivery_kind=excluded.delivery_kind,
emulator_path=excluded.emulator_path,
enabled=1,
version=CASE WHEN bios_requirements.catalog_digest!=excluded.catalog_digest
  THEN bios_requirements.version+1 ELSE bios_requirements.version END,
updated_at_ms=excluded.updated_at_ms
`,
			id,
			requirement.coreID,
			target.providerID,
			target.targetID,
			requirement.logical,
			requirement.mode,
			requirement.condition,
			nullableOptions(requirement.options),
			hex.EncodeToString(digest[:]),
			nullablePositive(requirement.size),
			requirement.md5,
			nullableStringValue(requirement.sha256),
			requirement.sourceURL,
			versionName,
			now.UnixMilli(),
			now.UnixMilli(),
			delivery,
			nullableStringValue(requirement.emulatorPath),
		)
		if err != nil {
			return fmt.Errorf("seed BIOS requirement: %w", err)
		}
	}
	return nil
}

func validateBIOSActivationOptions(catalog []staticBIOS) error {
	byCore := make(map[string]map[string]string)
	for _, requirement := range catalog {
		if err := validateBIOSDelivery(requirement); err != nil {
			return err
		}
		options, err := decodeBIOSOptions(requirement)
		if err != nil {
			return err
		}
		if err := mergeBIOSOptions(byCore, requirement, options); err != nil {
			return err
		}
	}
	return nil
}

func validateBIOSDelivery(requirement staticBIOS) error {
	delivery := requirement.delivery
	if delivery == "" {
		delivery = "BIOS_BUNDLE"
	}
	valid := delivery == "BIOS_BUNDLE" && requirement.emulatorPath == "" ||
		delivery == "EXTERNAL_FILE" && validEmulatorPath(requirement.emulatorPath)
	if !valid || requirement.size < 0 || requirement.sha256 != "" && len(requirement.sha256) != 64 {
		return fmt.Errorf("%w: %s/%s delivery", errBIOSOptions, requirement.coreID, requirement.logical)
	}
	return nil
}

func decodeBIOSOptions(requirement staticBIOS) (map[string]string, error) {
	if requirement.options == "" {
		return map[string]string{}, nil
	}
	var options map[string]string
	if err := json.Unmarshal([]byte(requirement.options), &options); err != nil || len(options) > 8 {
		return nil, fmt.Errorf("%w: %s/%s", errBIOSOptions, requirement.coreID, requirement.logical)
	}
	return options, nil
}

func mergeBIOSOptions(
	byCore map[string]map[string]string,
	requirement staticBIOS,
	options map[string]string,
) error {
	if len(options) == 0 {
		return nil
	}
	if byCore[requirement.coreID] == nil {
		byCore[requirement.coreID] = make(map[string]string)
	}
	for name, value := range options {
		if !validASCIIOption(name, 1) || !validASCIIOption(value, 0) {
			return fmt.Errorf("%w: %s/%s", errBIOSOptions, requirement.coreID, requirement.logical)
		}
		if existing, ok := byCore[requirement.coreID][name]; ok && existing != value {
			return fmt.Errorf("%w: %s/%s", errBIOSOptions, requirement.coreID, name)
		}
		byCore[requirement.coreID][name] = value
	}
	return nil
}

func validEmulatorPath(value string) bool {
	if len(value) < 1 || len(value) > 512 || value[0] != '/' || strings.ContainsAny(value, "\\?#\x00") ||
		strings.Contains(value, "//") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func nullablePositive(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableStringValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func validASCIIOption(value string, minimum int) bool {
	if len(value) < minimum || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func nullableJSON(value string) string {
	if value == "" {
		return "null"
	}
	return value
}

func nullableOptions(value string) any {
	if value == "" {
		return nil
	}
	return value
}
