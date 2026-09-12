package platformcatalog

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"retrom/internal/contentprofile"
)

const Version = 27

var ErrInvalid = errors.New("PLATFORM_CATALOG_INVALID")

type DirectoryTemplate struct {
	Key           string
	PlatformID    string
	DefaultCoreID string
	Name          string
	Description   string
	CatalogOrder  int
}

type Catalog struct {
	Version   int
	Templates []DirectoryTemplate
}

var current = Catalog{Version: Version, Templates: []DirectoryTemplate{
	{Key: "nes/fceumm", PlatformID: "nes", DefaultCoreID: "fceumm", Name: "NES 游戏", CatalogOrder: 10},
	{Key: "snes/snes9x", PlatformID: "snes", DefaultCoreID: "snes9x", Name: "SNES 游戏", CatalogOrder: 20},
	{Key: "gbc/gambatte", PlatformID: "gbc", DefaultCoreID: "gambatte", Name: "Game Boy 游戏", CatalogOrder: 30},
	{Key: "gba/mgba", PlatformID: "gba", DefaultCoreID: "mgba", Name: "GBA 游戏", CatalogOrder: 40},
	{Key: "arcade/fbneo", PlatformID: "arcade", DefaultCoreID: "fbneo", Name: "FBNeo 游戏", CatalogOrder: 50},
	{
		Key: "arcade/mame2003_plus", PlatformID: "arcade", DefaultCoreID: "mame2003_plus",
		Name: "MAME 2003 Plus 游戏", CatalogOrder: 60,
	},
	{
		Key: "arcade/fbalpha2012_cps1", PlatformID: "arcade", DefaultCoreID: "fbalpha2012_cps1",
		Name: "FB Alpha 2012 CPS-1 游戏", CatalogOrder: 70,
	},
	{
		Key: "arcade/fbalpha2012_cps2", PlatformID: "arcade", DefaultCoreID: "fbalpha2012_cps2",
		Name: "FB Alpha 2012 CPS-2 游戏", CatalogOrder: 80,
	},
	{Key: "dos/dosbox_pure", PlatformID: "dos", DefaultCoreID: "dosbox_pure", Name: "DOS 经典游戏", CatalogOrder: 90},
	{Key: "nds/desmume2015", PlatformID: "nds", DefaultCoreID: "desmume2015", Name: "Nintendo DS 游戏", CatalogOrder: 100},
	{
		Key: "atari2600/stella2014", PlatformID: "atari2600", DefaultCoreID: "stella2014",
		Name: "Atari 2600 游戏", CatalogOrder: 110,
	},
	{Key: "atari5200/a5200", PlatformID: "atari5200", DefaultCoreID: "a5200", Name: "Atari 5200 游戏", CatalogOrder: 120},
	{
		Key: "atari7800/prosystem", PlatformID: "atari7800", DefaultCoreID: "prosystem",
		Name: "Atari 7800 游戏", CatalogOrder: 130,
	},
	{Key: "lynx/handy", PlatformID: "lynx", DefaultCoreID: "handy", Name: "Atari Lynx 游戏", CatalogOrder: 140},
	{
		Key: "megadrive/genesis_plus_gx", PlatformID: "megadrive", DefaultCoreID: "genesis_plus_gx",
		Name: "Mega Drive 游戏", CatalogOrder: 150,
	},
	{Key: "pce/mednafen_pce", PlatformID: "pce", DefaultCoreID: "mednafen_pce", Name: "PC Engine 游戏", CatalogOrder: 160},
	{
		Key: "ngpc/mednafen_ngp", PlatformID: "ngpc", DefaultCoreID: "mednafen_ngp",
		Name: "Neo Geo Pocket 游戏", CatalogOrder: 170,
	},
	{
		Key: "n64/mupen64plus_next", PlatformID: "n64", DefaultCoreID: "mupen64plus_next",
		Name: "Nintendo 64 游戏", CatalogOrder: 180,
	},
	{Key: "psx/pcsx_rearmed", PlatformID: "psx", DefaultCoreID: "pcsx_rearmed", Name: "PlayStation 游戏", CatalogOrder: 190},
	{Key: "saturn/yabause", PlatformID: "saturn", DefaultCoreID: "yabause", Name: "Sega Saturn 游戏", CatalogOrder: 200},
	{Key: "dreamcast/flycast", PlatformID: "dreamcast", DefaultCoreID: "flycast", Name: "Dreamcast 游戏", CatalogOrder: 205},
	{Key: "pcfx/mednafen_pcfx", PlatformID: "pcfx", DefaultCoreID: "mednafen_pcfx", Name: "PC-FX 游戏", CatalogOrder: 210},
	{Key: "3do/opera", PlatformID: "3do", DefaultCoreID: "opera", Name: "3DO 游戏", CatalogOrder: 220},
	{Key: "psp/ppsspp", PlatformID: "psp", DefaultCoreID: "ppsspp", Name: "PSP 游戏", CatalogOrder: 230},
	// Play! 目前运行不稳定，因此不提供 ps2/play 推荐目录；用户仍可手动创建 PS2/Play! 游戏目录。
	{
		Key: "virtualboy/beetle_vb", PlatformID: "virtualboy", DefaultCoreID: "beetle_vb",
		Name: "Virtual Boy 游戏", CatalogOrder: 240,
	},
	{
		Key: "wonderswan/mednafen_wswan", PlatformID: "wonderswan", DefaultCoreID: "mednafen_wswan",
		Name: "WonderSwan 游戏", CatalogOrder: 250,
	},
	{
		Key: "mastersystem/smsplus", PlatformID: "mastersystem", DefaultCoreID: "smsplus",
		Name: "Master System 游戏", CatalogOrder: 260,
	},
	{
		Key: "nintendo3ds/azahar", PlatformID: "nintendo3ds", DefaultCoreID: "azahar",
		Name: "Nintendo 3DS 游戏", CatalogOrder: 270,
	},
	{Key: "rpgmaker/rpgmaker", PlatformID: "rpgmaker", DefaultCoreID: "rpgmaker", Name: "RPG Maker 游戏", CatalogOrder: 280},
	{
		Key: "ons/onscripter_yuri", PlatformID: "ons", DefaultCoreID: "onscripter_yuri",
		Name: "ONS 游戏", CatalogOrder: 290,
	},
	{
		Key: "kirikiri/kirikiri2", PlatformID: "kirikiri", DefaultCoreID: "kirikiri2",
		Name: "KiriKiri 游戏", CatalogOrder: 300,
	},
	{
		Key: "butterscotch/butterscotch", PlatformID: "butterscotch", DefaultCoreID: "butterscotch",
		Name: "GameMaker 游戏", CatalogOrder: 310,
	},
	{
		Key: "tyranoscript/tyranoscript", PlatformID: "tyranoscript", DefaultCoreID: "tyranoscript",
		Name: "TyranoScript 游戏", CatalogOrder: 320,
	},
	{
		Key: "wasm4/wasm4", PlatformID: "wasm4", DefaultCoreID: "wasm4",
		Name: "WASM-4 游戏", CatalogOrder: 330,
	},
	{
		Key: "j2me/j2me", PlatformID: "j2me", DefaultCoreID: "j2me",
		Name: "Java ME 游戏", CatalogOrder: 340,
	},
	{
		Key: "tic80/tic80", PlatformID: "tic80", DefaultCoreID: "tic80",
		Name: "TIC-80 游戏", CatalogOrder: 350,
	},
	{
		Key: "pico8/fake08", PlatformID: "pico8", DefaultCoreID: "fake08",
		Name: "PICO-8 游戏", CatalogOrder: 360,
	},
	{Key: "scummvm/scummvm", PlatformID: "scummvm", DefaultCoreID: "scummvm", Name: "ScummVM 游戏", CatalogOrder: 370},
	{
		Key: "zxspectrum/fuse", PlatformID: "zxspectrum", DefaultCoreID: "fuse",
		Name: "ZX Spectrum 游戏", CatalogOrder: 380,
	},
	{
		Key: "c64/vice_x64sc", PlatformID: "c64", DefaultCoreID: "vice_x64sc",
		Name: "Commodore 64 游戏", CatalogOrder: 390,
	},
	{
		Key: "colecovision/gearcoleco", PlatformID: "colecovision", DefaultCoreID: "gearcoleco",
		Name: "ColecoVision 游戏", CatalogOrder: 400,
	},
	{
		Key: "atarijaguar/virtualjaguar", PlatformID: "atarijaguar", DefaultCoreID: "virtualjaguar",
		Name: "Atari Jaguar 游戏", CatalogOrder: 410,
	},
	{
		Key: "doom/prboom", PlatformID: "doom", DefaultCoreID: "prboom",
		Name: "Doom 游戏", CatalogOrder: 420,
	},
	{
		Key: "c128/vice_x128", PlatformID: "c128", DefaultCoreID: "vice_x128",
		Name: "Commodore 128 游戏", CatalogOrder: 430,
	},
	{
		Key: "vic20/vice_xvic", PlatformID: "vic20", DefaultCoreID: "vice_xvic",
		Name: "Commodore VIC-20 游戏", CatalogOrder: 440,
	},
	{
		Key: "amiga/puae", PlatformID: "amiga", DefaultCoreID: "puae",
		Name: "Commodore Amiga 游戏", CatalogOrder: 450,
	},
	{Key: "flash/ruffle", PlatformID: "flash", DefaultCoreID: "ruffle", Name: "Flash 游戏", CatalogOrder: 460},
	{Key: "msx/webmsx", PlatformID: "msx", DefaultCoreID: "webmsx", Name: "MSX 游戏", CatalogOrder: 470},
	{Key: "x68000/px68k", PlatformID: "x68000", DefaultCoreID: "px68k", Name: "Sharp X68000 游戏", CatalogOrder: 480},
	{Key: "pc98/np2kai", PlatformID: "pc98", DefaultCoreID: "np2kai", Name: "PC-98 游戏", CatalogOrder: 490},
	{Key: "openbor/openbor", PlatformID: "openbor", DefaultCoreID: "openbor", Name: "OpenBOR 游戏", CatalogOrder: 500},
	{
		Key: "pcecd/mednafen_pce", PlatformID: "pcecd", DefaultCoreID: "mednafen_pce",
		Name: "PC Engine CD 游戏", CatalogOrder: 510,
	},
	{
		Key: "zx81/81", PlatformID: "zx81", DefaultCoreID: "81",
		Name: "ZX81 游戏", CatalogOrder: 520,
	},
	{
		Key: "amstradcpc/cap32", PlatformID: "amstradcpc", DefaultCoreID: "cap32",
		Name: "Amstrad CPC 游戏", CatalogOrder: 530,
	},
	{
		Key: "pet/vice_xpet", PlatformID: "pet", DefaultCoreID: "vice_xpet",
		Name: "Commodore PET 游戏", CatalogOrder: 540,
	},
	{
		Key: "plus4/vice_xplus4", PlatformID: "plus4", DefaultCoreID: "vice_xplus4",
		Name: "Commodore Plus/4 游戏", CatalogOrder: 550,
	},
	{
		Key: "cdi/same_cdi", PlatformID: "cdi", DefaultCoreID: "same_cdi",
		Name: "Philips CD-i 游戏", CatalogOrder: 560,
	},
	{Key: "vectrex/vecx", PlatformID: "vectrex", DefaultCoreID: "vecx", Name: "Vectrex 游戏", CatalogOrder: 570},
	{
		Key: "intellivision/freeintv", PlatformID: "intellivision", DefaultCoreID: "freeintv",
		Name: "Intellivision 游戏", CatalogOrder: 580,
	},
	{Key: "pc88/quasi88", PlatformID: "pc88", DefaultCoreID: "quasi88", Name: "PC-88 游戏", CatalogOrder: 590},
	{Key: "neogeocd/neocd", PlatformID: "neogeocd", DefaultCoreID: "neocd", Name: "Neo Geo CD 游戏", CatalogOrder: 600},
	{
		Key: "pokemini/gbe_plus", PlatformID: "pokemini", DefaultCoreID: "gbe_plus",
		Name: "Pokémon Mini 游戏", CatalogOrder: 610,
	},
	{Key: "cavestory/nxengine", PlatformID: "cavestory", DefaultCoreID: "nxengine", Name: "洞窟物语", CatalogOrder: 620},
	{
		Key: "gamegear/genesis_plus_gx", PlatformID: "gamegear", DefaultCoreID: "genesis_plus_gx",
		Name: "Game Gear 游戏", CatalogOrder: 700,
	},
	{
		Key: "sg1000/genesis_plus_gx", PlatformID: "sg1000", DefaultCoreID: "genesis_plus_gx",
		Name: "SG-1000 游戏", CatalogOrder: 710,
	},
	{
		Key: "multivision/genesis_plus_gx", PlatformID: "multivision", DefaultCoreID: "genesis_plus_gx",
		Name: "Multivision 游戏", CatalogOrder: 720,
	},
	{
		Key: "pico/picodrive", PlatformID: "pico", DefaultCoreID: "picodrive",
		Name: "Sega Pico 游戏", CatalogOrder: 730,
	},
	{
		Key: "sega32x/picodrive", PlatformID: "sega32x", DefaultCoreID: "picodrive",
		Name: "Sega 32X 游戏", CatalogOrder: 740,
	},
	{
		Key: "supergrafx/mednafen_pce", PlatformID: "supergrafx", DefaultCoreID: "mednafen_pce",
		Name: "SuperGrafx 游戏", CatalogOrder: 750,
	},
	{
		Key: "gx4000/cap32", PlatformID: "gx4000", DefaultCoreID: "cap32",
		Name: "GX4000 游戏", CatalogOrder: 760,
	},
}}

func Current() Catalog {
	result := current
	result.Templates = append([]DirectoryTemplate(nil), current.Templates...)
	return result
}

func Validate(catalog Catalog) error {
	if catalog.Version < 1 || len(catalog.Templates) == 0 {
		return fmt.Errorf("%w: empty catalog", ErrInvalid)
	}
	keys := make(map[string]struct{}, len(catalog.Templates))
	pairs := make(map[string]struct{}, len(catalog.Templates))
	orders := make(map[int]struct{}, len(catalog.Templates))
	lastOrder := 0
	for _, template := range catalog.Templates {
		if err := validateTemplate(template, lastOrder, keys, pairs, orders); err != nil {
			return err
		}
		pair := template.PlatformID + "/" + template.DefaultCoreID
		keys[template.Key] = struct{}{}
		pairs[pair] = struct{}{}
		orders[template.CatalogOrder] = struct{}{}
		lastOrder = template.CatalogOrder
	}
	return nil
}

func validateTemplate(
	template DirectoryTemplate,
	lastOrder int,
	keys, pairs map[string]struct{},
	orders map[int]struct{},
) error {
	pair := template.PlatformID + "/" + template.DefaultCoreID
	if template.Key != pair || template.Key != strings.ToLower(template.Key) ||
		template.CatalogOrder <= 0 || template.CatalogOrder <= lastOrder ||
		!validText(template.Name, 1, 200) || !validText(template.Description, 0, 10_000) {
		return fmt.Errorf("%w: invalid template %q", ErrInvalid, template.Key)
	}
	if _, exists := keys[template.Key]; exists {
		return fmt.Errorf("%w: duplicate key %q", ErrInvalid, template.Key)
	}
	if _, exists := pairs[pair]; exists {
		return fmt.Errorf("%w: duplicate pair %q", ErrInvalid, pair)
	}
	if _, exists := orders[template.CatalogOrder]; exists {
		return fmt.Errorf("%w: duplicate order %d", ErrInvalid, template.CatalogOrder)
	}
	// Directory-style runtimes accept project folders in addition to archives;
	// their importer validates the full project shape.
	if _, project := contentprofile.ProjectKind(template.PlatformID); !project &&
		!validExtensions(contentprofile.SupportedExtensions(template.PlatformID)) {
		return fmt.Errorf("%w: extensions for %q", ErrInvalid, template.PlatformID)
	}
	return nil
}

func validText(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) &&
		utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum
}

func validExtensions(extensions []string) bool {
	if len(extensions) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		if len(extension) < 2 || extension[0] != '.' || extension != strings.ToLower(extension) {
			return false
		}
		if strings.Contains(extension, "..") || strings.HasSuffix(extension, ".") {
			return false
		}
		for _, character := range extension[1:] {
			if character == '.' {
				continue
			}
			if character < 'a' || character > 'z' {
				if character < '0' || character > '9' {
					return false
				}
			}
		}
		if _, exists := seen[extension]; exists {
			return false
		}
		seen[extension] = struct{}{}
	}
	return true
}
