package contentprofile

import (
	"errors"
	"path"
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
	ContentKindButterscotchProject ContentKind = "BUTTERSCOTCH_PROJECT"
	ContentKindTyranoScriptProject ContentKind = "TYRANOSCRIPT_PROJECT"
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
	"nes":          single("nes", ".nes", ".unf", ".unif", ".fds"),
	"fds":          single("fds", ".fds"),
	"snes":         single("snes", ".sfc", ".smc", ".swc", ".fig"),
	"gbc":          single("gbc", ".gb", ".gbc", ".dmg"),
	"gba":          single("gba", ".gba"),
	"nds":          single("nds", ".nds"),
	"atari5200":    single("atari5200", ".a52"),
	"psx":          raw("psx", ".chd"),
	"lynx":         single("lynx", ".lnx"),
	"saturn":       withContentKinds(raw("saturn", ".chd"), ContentKindSingleFile, ContentKindMultiDisc),
	"megadrive":    single("megadrive", ".md"),
	"n64":          single("n64", ".z64"),
	"3do":          raw("3do", ".chd"),
	"atari7800":    single("atari7800", ".a78"),
	"atari2600":    single("atari2600", ".a26"),
	"pce":          single("pce", ".pce"),
	"pcfx":         raw("pcfx", ".chd"),
	"ngpc":         single("ngpc", ".ngp"),
	"psp":          raw("psp", ".iso", ".cso"),
	"virtualboy":   single("virtualboy", ".vb"),
	"wonderswan":   single("wonderswan", ".ws", ".wsc"),
	"mastersystem": single("mastersystem", ".sms"),
	"nintendo3ds":  raw("nintendo3ds", ".3ds", ".cci"),
	"wasm4":        single("wasm4", ".wasm"),

	"rpgmaker":     project("rpgmaker", ContentKindRPGMakerProject),
	"ons":          project("ons", ContentKindONSProject),
	"kirikiri":     project("kirikiri", ContentKindKiriKiriProject),
	"butterscotch": project("butterscotch", ContentKindButterscotchProject),
	"tyranoscript": project("tyranoscript", ContentKindTyranoScriptProject,
		ArchiveNWJSExecutable, ArchiveElectronASAR),
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
	extension := strings.ToLower(path.Ext(logicalName))
	for _, allowed := range profile.Extensions {
		if extension == allowed {
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
