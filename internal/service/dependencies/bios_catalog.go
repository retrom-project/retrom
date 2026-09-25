package dependencies

import (
	"context"
	"crypto/sha256"
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
	members      string
	sourceDigest string
	providerID   string
	targetID     string
}

var staticBIOSCatalog = append(pc88BIOSCatalog(), []staticBIOS{
	{
		coreID: "apple2js", logical: "AppleIIe.rom", mode: "REQUIRED", size: 16384,
		md5: "38063e08c778503fc03ecebb979769e9", sha256: "aab38a03ca8deabbb2f868733148c2efd6f655a59cd9c5d058ef3e0b7aa86a1a",
		sourceURL: "https://github.com/whscullin/apple2js/tree/ee0aed25f73c69d0245e86a2a5fccb3324c3056c/js/roms",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/roms/AppleIIe.rom",
	},
	{
		coreID: "apple2js", logical: "apple2e-character.rom", mode: "REQUIRED", size: 4096,
		md5: "9123fff3442c0e688cc6816be88dd4ab", sha256: "52c3b87900ac939f6525402cab1ccfd8f8259290fc6df54da48fb4c98ae3ed0f",
		sourceURL: "https://github.com/whscullin/apple2js/tree/ee0aed25f73c69d0245e86a2a5fccb3324c3056c/js/roms",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/roms/apple2e-character.rom",
	},
	{
		coreID: "apple2js", logical: "AppleIIe_DiskII.rom", mode: "REQUIRED", size: 256,
		md5: "2020aa1413ff77fe29353f3ee72dc295", sha256: "de1e3e035878bab43d0af8fe38f5839c527e9548647036598ee6fe7ec74d2a7d",
		sourceURL: "https://github.com/whscullin/apple2js/tree/ee0aed25f73c69d0245e86a2a5fccb3324c3056c/js/roms",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/roms/AppleIIe_DiskII.rom",
	},
	{
		coreID: "genesis_plus_gx", logical: "bios_CD_E.bin", mode: "CONDITIONAL", condition: "SEGA_CD_CONTENT", size: 131072,
		md5: "e66fa1dc5820d254611fdcdba0662372", sourceURL: "https://emulatorjs.org/docs/systems/sega-cd/",
		providerID: "emulatorjs", targetID: "genesis-plus-gx-cd",
	},
	{
		coreID: "genesis_plus_gx", logical: "bios_CD_U.bin", mode: "CONDITIONAL", condition: "SEGA_CD_CONTENT", size: 131072,
		md5: "2efd74e3232ff260e371b99f84024f7f", sourceURL: "https://emulatorjs.org/docs/systems/sega-cd/",
		providerID: "emulatorjs", targetID: "genesis-plus-gx-cd",
	},
	{
		coreID: "genesis_plus_gx", logical: "bios_CD_J.bin", mode: "CONDITIONAL", condition: "SEGA_CD_CONTENT", size: 131072,
		md5: "278a9397d192149e84e820ac621a8edd", sourceURL: "https://emulatorjs.org/docs/systems/sega-cd/",
		providerID: "emulatorjs", targetID: "genesis-plus-gx-cd",
	},
	{
		coreID: "puae", logical: "kick34005.A500", mode: "OPTIONAL", condition: "AMIGA_COMPUTER_CONTENT", size: 262144,
		md5: "82a21c1890cae844b3df741f2762d48d", sourceURL: "https://docs.libretro.com/library/puae/",
	},
	{
		coreID: "puae", logical: "kick40068.A1200", mode: "OPTIONAL", condition: "AMIGA_COMPUTER_CONTENT", size: 524288,
		md5: "646773759326fbac3b2311fd8c8793ee", sourceURL: "https://docs.libretro.com/library/puae/",
	},
	{
		coreID: "puae", logical: "kick40060.CD32", mode: "CONDITIONAL", condition: "AMIGA_CD32_CONTENT", size: 524288,
		md5: "5f8924d013dd57a89cf349f4cdedc6b1", sourceURL: "https://docs.libretro.com/library/puae/",
	},
	{
		coreID: "puae", logical: "kick40060.CD32.ext", mode: "CONDITIONAL", condition: "AMIGA_CD32_CONTENT", size: 524288,
		md5: "bb72565701b1b6faece07d68ea5da639", sourceURL: "https://docs.libretro.com/library/puae/",
	},
	{
		coreID: "jsbeeb", logical: "os.rom", mode: "REQUIRED", size: 16384,
		md5: "0a59a5ba15fe8557b5f7fee32bbd393a", sha256: "2d9fea69017864f6962704481829f95fee08446c8c3a13826d5d4e44000ac9de",
		sourceURL: "https://github.com/mattgodbolt/jsbeeb/blob/c4839b888af29777390a5534f69d8ef8189bbed9/public/roms/README",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/roms/os.rom",
	},
	{
		coreID: "jsbeeb", logical: "BASIC.ROM", mode: "REQUIRED", size: 16384,
		md5: "2cc67be4624df4dc66617742571a8e3d", sha256: "45bd55dc0f6f0f8f1fe9e2481de7def206565eec8f600ba3068b849ca4132079",
		sourceURL: "https://github.com/mattgodbolt/jsbeeb/blob/c4839b888af29777390a5534f69d8ef8189bbed9/public/roms/README",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/roms/BASIC.ROM",
	},
	{
		coreID: "jsbeeb", logical: "DFS-1.2.rom", mode: "REQUIRED", size: 16384,
		md5: "5daed103918277e2065dd7e8d23e57a5", sha256: "e745e34895225a6650b712c1dd0656cb0b0b15f072a8ae6d9ea8d1ac257eb3d6",
		sourceURL: "https://github.com/mattgodbolt/jsbeeb/blob/c4839b888af29777390a5534f69d8ef8189bbed9/public/roms/README",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/roms/b/DFS-1.2.rom",
	},
	{
		coreID: "samcoupeweb", logical: "samcoupe.rom", mode: "REQUIRED", size: 32768,
		md5: "ad08ed47b07b0d7047fd3d0b5e7d90b3", sha256: "e7ec9cd06fb2d479a807cf1739e75f3ef4975c12869be3d3a991232c57d1688d",
		sourceURL: "https://github.com/anomixer/SamCoupeWeb/blob/" +
			"6593bda819830c07bd48e0dffd32d3b3ef525fb0/Resource/samcoupe.rom",
		delivery: "EXTERNAL_FILE", emulatorPath: "/Resource/samcoupe.rom",
	},
	{
		coreID: "freechaf", logical: "sl31253.bin", mode: "REQUIRED", size: 1024,
		md5: "ac9804d4c0e9d07e33472e3726ed15c3", sha256: "876a263d28d8f9e9f51785b91269393f06599debae9ba2ef1cba67a283504e2c",
		sourceURL: "https://github.com/libretro/FreeChaF/blob/76c7a84f1f7e80f3e6f2bba96fe100cb24e99124/README.md",
	},
	{
		coreID: "freechaf", logical: "sl31254.bin", mode: "REQUIRED", size: 1024,
		md5: "da98f4bb3242ab80d76629021bb27585", sha256: "a8d2c0d958b8ea0ee2855b5416993c149a3050cb3c85b781404d3c2a233defc1",
		sourceURL: "https://github.com/libretro/FreeChaF/blob/76c7a84f1f7e80f3e6f2bba96fe100cb24e99124/README.md",
	},
	{
		coreID: "freechaf", logical: "sl90025.bin", mode: "OPTIONAL", size: 1024,
		md5: "95d339631d867c8f1d15a5f2ec26069d", sha256: "bf3755d849438034f3ff92bde357310074c09088b22cf33e512ec06c65d4eaab",
		sourceURL: "https://github.com/libretro/FreeChaF/blob/76c7a84f1f7e80f3e6f2bba96fe100cb24e99124/README.md",
	},
	{
		coreID: "o2em", logical: "o2rom.bin", mode: "REQUIRED", size: 1024,
		md5: "562d5ebf9e030a40d6fabfc2f33139fd", sha256: "cb0c5d9ed64f7c1d8870333451832638885b9aa3d7013f0c05fd2a20a5e5bfef",
		sourceURL: "https://docs.libretro.com/library/o2em/",
	},
	{
		coreID: "gam4980", logical: "8.BIN", mode: "REQUIRED", size: 2097152,
		md5: "ea26b08e67511a34460c103b8b669154", sha256: "7663735609c416025b2738c80cedaf11528ff8cb5a7c74c7c31c1f46e43e9caf",
		sourceURL: "https://github.com/ThisBoringWorld/gam4980/tree/eeaa531b55e7127ab4b5e0bdc5ceba686df59c6a",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/gam4980/8.BIN",
	},
	{
		coreID: "gam4980", logical: "E.BIN", mode: "REQUIRED", size: 2097152,
		md5: "f812738e5ae75de0f4faae78a3829866", sha256: "3e12d40948fd50710cef8c6d14acea26ad69f47312125a61bd1003912893fcad",
		sourceURL: "https://github.com/ThisBoringWorld/gam4980/tree/eeaa531b55e7127ab4b5e0bdc5ceba686df59c6a",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/gam4980/E.BIN",
	},
	{
		coreID: "freeintv", logical: "exec.bin", mode: "REQUIRED", size: 8192,
		md5: "62e761035cb657903761800f4437b8af", sha256: "1aeb614856beba95463166daf09304b414d5617d3f37d221724b3337fc4b2722",
		sourceURL: "https://docs.libretro.com/library/freeintv/",
	},
	{
		coreID: "freeintv", logical: "grom.bin", mode: "REQUIRED", size: 2048,
		md5: "0cd5946c6473e42e8e4c2137785e427f", sha256: "a80b6841182547d08635ad30a6af71441c4c9eed9391b3dd22feb30d8e50cc85",
		sourceURL: "https://docs.libretro.com/library/freeintv/",
	},
	{
		coreID: "neocd", logical: "neocd.bin", mode: "REQUIRED", size: 524288,
		md5: "f39572af7584cb5b3f70ae8cc848aba2", sha256: "2e93af5848080ea04d17a7841b742f009330d30e4ff40c3410547581d921c892",
		sourceURL: "https://github.com/libretro/neocd_libretro/blob/3118c6901787e863e80e79170d02d47657b3b0ab/README.md",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/neocd/neocd.bin",
	},
	{
		delivery: "EXTERNAL_FILE", emulatorPath: "/bios.min",
		coreID: "gbe_plus", logical: "bios.min", mode: "REQUIRED", size: 4096,
		md5: "1e4fb124a3a886865acb574f388c803d", sha256: "45a1c7f28b9ad585e67f047abe9c1c956724bfcab8c9011002af4274e7c50e8f",
		sourceURL: "https://docs.libretro.com/library/pokemini/",
	},
	{
		delivery: "EXTERNAL_FILE", emulatorPath: "/game/keropi/iplrom.dat",
		coreID: "px68k", logical: "iplrom.dat", mode: "REQUIRED", size: 131072,
		md5: "7fd4caabac1d9169e289f0f7bbf71d8e", sha256: "8ead1d0f4ebb9c59a7fa118596f819e191c310442a00c56ab5ec5e9e7a189677",
		sourceURL: "https://docs.libretro.com/library/px68k/",
	},
	{
		delivery: "EXTERNAL_FILE", emulatorPath: "/game/keropi/cgrom.dat",
		coreID: "px68k", logical: "cgrom.dat", mode: "REQUIRED", size: 786432,
		md5: "cb0a5cfcf7247a7eab74bb2716260269", sha256: "c4e47e1480af0b00b330a49650480c8caa34054a6e97db7ae03cbade9890185d",
		sourceURL: "https://docs.libretro.com/library/px68k/",
	},
	{
		coreID: "flycast", logical: "dc_boot.bin", mode: "REQUIRED", size: 2097152,
		md5: "e10c53c2f8b90bab96ead2d368858623", sha256: "88d6a666495ad14ab5988d8cb730533cfc94ec2cfd53a7eeda14642ab0d4abf9",
		sourceURL: "https://docs.libretro.com/library/flycast/",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/dc/dc_boot.bin",
		options: `{"reicast_hle_bios":"disabled"}`,
	},
	{
		// Flash contains mutable console settings; accept the correct size without a fixed digest.
		coreID: "flycast", logical: "dc_flash.bin", mode: "REQUIRED", size: 131072,
		sourceURL: "https://github.com/nasomers/flycast-wasm/tree/v1.0",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/dc/dc_flash.bin",
	},
	{
		coreID: "flycast-naomi", logical: "naomi.zip", mode: "REQUIRED",
		sourceURL: "https://docs.libretro.com/library/flycast/",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/dc/naomi.zip",
	},
	{
		coreID: "flycast-naomi2", logical: "naomi2.zip", mode: "REQUIRED",
		sourceURL: "https://docs.libretro.com/library/flycast/",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/dc/naomi2.zip",
	},
	{
		coreID: "flycast-atomiswave", logical: "awbios.zip", mode: "REQUIRED",
		sourceURL: "https://docs.libretro.com/library/flycast/",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/dc/awbios.zip",
	},
	{
		coreID: "mednafen_pce", logical: "syscard3.pce", mode: "CONDITIONAL", condition: "PCE_CD_CONTENT", size: 262144,
		md5: "38179df8f4ac870017db21ebcbf53114", sha256: "e11527b3b96ce112a037138988ca72fd117a6b0779c2480d9e03eaebece3d9ce",
		sourceURL: "https://docs.libretro.com/library/beetle_pce_fast/",
	},
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
		coreID: "gearcoleco", logical: "colecovision.rom", mode: "REQUIRED", size: 8192,
		md5: "2c66f5911e5b42b8ebe113403548eee7", sha256: "990bf1956f10207d8781b619eb74f89b00d921c8d45c95c334c16c8cceca09ad",
		sourceURL: "https://docs.libretro.com/library/gearcoleco/",
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
		coreID: "prboom", logical: "prboom.wad", mode: "REQUIRED", size: 143312,
		md5: "72ae1b47820fcc93cc0df9c428d0face", sha256: "b4dd3642932193cc42bca0ee98bf30004888ca4850d69e85023b8baacfba1d1d",
		sourceURL: "https://docs.libretro.com/library/prboom/",
	},
	{
		coreID: "mednafen_pcfx", logical: "pcfx.rom", mode: "REQUIRED", size: 1048576,
		md5: "08e36edbea28a017f79f8d4f7ff9b6d7", sha256: "4b44ccf5d84cc83daa2e6a2bee00fdafa14eb58bdf5859e96d8861a891675417",
		sourceURL: "https://docs.libretro.com/library/beetle_pc_fx/",
	},
}...)

// Static BIOS definitions are synchronized atomically with their aliases and version provenance.
func bootstrapStaticBIOS(
	ctx context.Context,
	records BIOSRecords,
	versionName string,
	selectedTargets map[string]RuntimeTarget,
	now time.Time,
) error {
	catalog, err := completeStaticBIOSCatalog()
	if err != nil {
		return err
	}
	if err := validateBIOSActivationOptions(catalog); err != nil {
		return err
	}
	for _, requirement := range catalog {
		target, selected := selectedTargets[requirement.coreID]
		if !selected {
			continue
		}
		if requirement.providerID != "" &&
			(target.ProviderID != requirement.providerID || target.TargetID != requirement.targetID) {
			return fmt.Errorf("%w: firmware target %s", errBIOSOptions, requirement.coreID)
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
				"archiveMembers":    json.RawMessage(nullableJSON(requirement.members)),
				"sourceDigest":      requirement.sourceDigest,
				"md5":               requirement.md5,
				"mode":              requirement.mode,
				"sha256":            nullableStringValue(requirement.sha256),
				"sizeBytes":         nullablePositive(requirement.size),
			},
		)
		digest := sha256.Sum256(canonical)
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(
			"retrom:bios:"+target.ProviderID+":"+target.TargetID+":"+requirement.logical,
		)).String()
		err := records.Upsert(ctx, BIOSRequirement{
			ID: id, CoreID: requirement.coreID, ProviderID: target.ProviderID, TargetID: target.TargetID,
			LogicalName: requirement.logical, Mode: requirement.mode, ConditionCode: requirement.condition,
			Options: nullableOptions(
				requirement.options,
			), Digest: hex.EncodeToString(
				digest[:],
			), SizeBytes: nullablePositive(
				requirement.size,
			),
			MD5: requirement.md5, SHA256: nullableStringValue(requirement.sha256), SourceURL: requirement.sourceURL,
			VersionName: versionName, AtMS: now.UnixMilli(), Delivery: delivery, EmulatorPath: nullableStringValue(
				requirement.emulatorPath,
			),
			ArchiveMembers: nullableStringValue(requirement.members),
		})
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

func nullablePositive(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func nullableStringValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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

func nullableOptions(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
