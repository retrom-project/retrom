package contentprofile

import (
	"errors"
	"strings"

	"retrom/internal/importing"
)

type ArchiveFormat string

type ArchivePolicy string

type ContentKind string

const (
	ArchiveZIP            ArchiveFormat = "ZIP"
	ArchiveSevenZip       ArchiveFormat = "SEVEN_Z"
	ArchiveNWJSExecutable ArchiveFormat = "NWJS_EXECUTABLE"
	ArchiveElectronASAR   ArchiveFormat = "ELECTRON_ASAR"

	ArchiveNone          ArchivePolicy = "NONE"
	ArchiveSinglePrimary ArchivePolicy = "SINGLE_PRIMARY"
	ArchiveProject       ArchivePolicy = "PROJECT"

	RawFileFormat             = "RAW_FILE"
	SingleArchiveMemberFormat = "SINGLE_ARCHIVE_MEMBER"

	ContentKindSingleFile          ContentKind = "SINGLE_FILE"
	ContentKindDOSBundle           ContentKind = "DOS_BUNDLE"
	ContentKindMultiDisc           ContentKind = "MULTI_DISC"
	ContentKindRPGMakerProject     ContentKind = "RPG_MAKER_PROJECT"
	ContentKindONSProject          ContentKind = "ONS_PROJECT"
	ContentKindKiriKiriProject     ContentKind = "KIRIKIRI_PROJECT"
	ContentKindNXEngineProject     ContentKind = "NXENGINE_PROJECT"
	ContentKindButterscotchProject ContentKind = "BUTTERSCOTCH_PROJECT"
	ContentKindTyranoScriptProject ContentKind = "TYRANOSCRIPT_PROJECT"
	ContentKindScummVMProject      ContentKind = "SCUMMVM_PROJECT"
)

var (
	ErrNoSupportedContent      = errors.New("NO_SUPPORTED_CONTENT")
	ErrAmbiguousPrimaryContent = errors.New("AMBIGUOUS_PRIMARY_CONTENT")
)

type Profile struct {
	PlatformID     string
	Extensions     []string
	ArchivePolicy  ArchivePolicy
	ArchiveFormats []ArchiveFormat
	FormatCode     string
	ContentKinds   []ContentKind
}

var registry = map[string]Profile{
	"gamegear":    single("gamegear", ".gg"),
	"sg1000":      single("sg1000", ".sg"),
	"multivision": single("multivision", ".sg"),
	"pico":        single("pico", ".md", ".bin"),
	"sega32x":     single("sega32x", ".32x"),
	"supergrafx":  single("supergrafx", ".pce", ".sgx"),
	"gx4000":      single("gx4000", ".cpr"),

	"neogeocd":      raw("neogeocd", ".chd"),
	"pokemini":      single("pokemini", ".min"),
	"vectrex":       single("vectrex", ".vec", ".bin"),
	"intellivision": single("intellivision", ".int", ".rom", ".bin"),
	"x68000":        single("x68000", ".dim", ".xdf", ".hdf"),
	"nes":           single("nes", ".nes", ".unf", ".unif", ".fds"),
	"fds":           single("fds", ".fds"),
	"snes":          single("snes", ".sfc", ".smc", ".swc", ".fig"),
	"gbc":           single("gbc", ".gb", ".gbc", ".dmg"),
	"gba":           single("gba", ".gba"),
	"nds":           single("nds", ".nds"),
	"atari5200":     single("atari5200", ".a52"),
	"psx":           raw("psx", ".chd"),
	"lynx":          single("lynx", ".lnx"),
	"dreamcast":     raw("dreamcast", ".chd"),
	"saturn":        withContentKinds(raw("saturn", ".chd"), ContentKindSingleFile, ContentKindMultiDisc),
	"megadrive":     single("megadrive", ".md", ".smd", ".bin"),
	"n64":           single("n64", ".z64"),
	"3do":           raw("3do", ".chd"),
	"atari7800":     single("atari7800", ".a78"),
	"atari2600":     single("atari2600", ".a26"),
	"pce":           single("pce", ".pce"),
	"pcecd":         raw("pcecd", ".chd"),
	"cdi":           raw("cdi", ".chd"),
	"zx81":          single("zx81", ".p", ".tzx", ".t81"),
	"amstradcpc":    single("amstradcpc", ".dsk", ".sna"),
	"pet":           single("pet", viceSingleFileExtensions...),
	"plus4":         single("plus4", viceSingleFileExtensions...),
	"pcfx":          raw("pcfx", ".chd"),
	"ngpc":          single("ngpc", ".ngp", ".ngc"),
	"psp":           raw("psp", ".iso", ".cso"),
	"pc88":          raw("pc88", ".d88", ".u88"),
	"pc98":          raw("pc98", ".hdi", ".d88"),
	"ps2":           raw("ps2", ".iso", ".chd"),
	"virtualboy":    single("virtualboy", ".vb"),
	"wonderswan":    single("wonderswan", ".ws", ".wsc"),
	"mastersystem":  single("mastersystem", ".sms"),
	"nintendo3ds":   raw("nintendo3ds", ".3ds", ".cci"),
	"j2me":          raw("j2me", ".jar"),
	"flash":         raw("flash", ".swf"),
	"msx":           single("msx", ".rom", ".mx1", ".mx2", ".dsk", ".cas"),
	"wasm4":         single("wasm4", ".wasm"),
	"zxspectrum":    single("zxspectrum", ".tzx", ".tap", ".z80", ".rzx", ".scl", ".trd"),
	"c64":           single("c64", viceSingleFileExtensions...),
	"c128":          single("c128", viceSingleFileExtensions...),
	"vic20":         single("vic20", viceSingleFileExtensions...),
	"colecovision":  single("colecovision", ".col", ".cv", ".bin", ".rom"),
	"atarijaguar":   single("atarijaguar", ".j64", ".jag", ".rom", ".abs", ".cof", ".bin", ".prg"),
	"doom":          single("doom", ".wad", ".iwad"),
	"amiga": single(
		"amiga", ".adf", ".adz", ".dms", ".fdi", ".ipf", ".raw",
		".hdf", ".hdz", ".lha", ".chd", ".nrg", ".iso",
	),
	"openbor": single("openbor", ".pak"),
	"tic80":   single("tic80", ".tic"),
	"pico8":   single("pico8", ".p8", ".p8.png"),

	"cavestory":    project("cavestory", ContentKindNXEngineProject),
	"scummvm":      project("scummvm", ContentKindScummVMProject),
	"rpgmaker":     project("rpgmaker", ContentKindRPGMakerProject),
	"ons":          project("ons", ContentKindONSProject),
	"kirikiri":     project("kirikiri", ContentKindKiriKiriProject),
	"butterscotch": project("butterscotch", ContentKindButterscotchProject),
	"tyranoscript": project("tyranoscript", ContentKindTyranoScriptProject,
		ArchiveNWJSExecutable, ArchiveElectronASAR),
}

var viceSingleFileExtensions = []string{
	".d64", ".d6z", ".d71", ".d7z", ".d80", ".d81", ".d82", ".d8z",
	".g64", ".g6z", ".g41", ".g4z", ".x64", ".x6z", ".nib", ".nbz",
	".d2m", ".d4m", ".t64", ".tap", ".tcrt", ".prg", ".p00", ".crt",
	".bin", ".vsf", ".gz", ".20", ".40", ".60", ".a0", ".b0", ".rom",
}

var specialPlatformExtensions = map[string][]string{
	"arcade": {".zip"},
	"dos":    {".exe", ".com", ".bat"},
}

func single(platformID string, extensions ...string) Profile {
	return Profile{
		PlatformID: platformID, Extensions: extensions, ArchivePolicy: ArchiveSinglePrimary,
		ArchiveFormats: []ArchiveFormat{ArchiveZIP, ArchiveSevenZip}, FormatCode: RawFileFormat,
		ContentKinds: []ContentKind{ContentKindSingleFile},
	}
}

func raw(platformID string, extensions ...string) Profile {
	return Profile{
		PlatformID: platformID, Extensions: extensions, ArchivePolicy: ArchiveNone,
		ArchiveFormats: nil, FormatCode: RawFileFormat, ContentKinds: []ContentKind{ContentKindSingleFile},
	}
}

func withContentKinds(profile Profile, kinds ...ContentKind) Profile {
	profile.ContentKinds = append([]ContentKind(nil), kinds...)
	return profile
}

func ByPlatform(platformID string) (Profile, bool) {
	profile, ok := registry[platformID]
	if !ok {
		return Profile{}, false
	}
	profile.Extensions = append([]string(nil), profile.Extensions...)
	profile.ArchiveFormats = append([]ArchiveFormat(nil), profile.ArchiveFormats...)
	profile.ContentKinds = append([]ContentKind(nil), profile.ContentKinds...)
	return profile, true
}

// SupportedExtensions returns the game payload extensions presented to users.
// Archive wrappers for single-ROM platforms are import transports and are not
// included; Arcade ZIP and DOS executable entries are the payload themselves.
func SupportedExtensions(platformID string) []string {
	if profile, ok := registry[platformID]; ok {
		if profile.ArchivePolicy == ArchiveProject {
			return projectExtensions(profile.ArchiveFormats)
		}
		return append([]string(nil), profile.Extensions...)
	}
	return append([]string(nil), specialPlatformExtensions[platformID]...)
}

func AllowsContentKind(platformID string, kind ContentKind) bool {
	profile, ok := registry[platformID]
	if !ok {
		return false
	}
	for _, allowed := range profile.ContentKinds {
		if kind == allowed {
			return true
		}
	}
	return false
}

// KnownContentKind describes supported payload semantics, independently of a
// product binding's narrower admission policy. DOS is handled by its dedicated
// bundle importer rather than a single-file/archive profile.
func KnownContentKind(kind ContentKind) bool {
	if kind == ContentKindDOSBundle {
		return true
	}
	for platformID := range registry {
		if AllowsContentKind(platformID, kind) {
			return true
		}
	}
	return false
}

func AcceptsRaw(platformID, logicalName string) bool {
	profile, ok := registry[platformID]
	if !ok {
		return false
	}
	name := strings.ToLower(logicalName)
	for _, allowed := range profile.Extensions {
		if strings.HasSuffix(name, allowed) {
			return true
		}
	}
	return false
}

func AcceptsArchive(platformID string, format ArchiveFormat) bool {
	profile, ok := registry[platformID]
	if !ok || profile.ArchivePolicy != ArchiveSinglePrimary && profile.ArchivePolicy != ArchiveProject {
		return false
	}
	for _, allowed := range profile.ArchiveFormats {
		if format == allowed {
			return true
		}
	}
	return false
}

func SelectArchivePrimary(platformID string, entries []importing.ArchiveEntry) (importing.ArchiveEntry, error) {
	candidates := make([]importing.ArchiveEntry, 0, 2)
	for _, entry := range entries {
		if AcceptsRaw(platformID, entry.NormalizedPath) {
			candidates = append(candidates, entry)
		}
	}
	switch len(candidates) {
	case 0:
		return importing.ArchiveEntry{}, ErrNoSupportedContent
	case 1:
		return candidates[0], nil
	default:
		return importing.ArchiveEntry{}, ErrAmbiguousPrimaryContent
	}
}
