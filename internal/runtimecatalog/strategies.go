package runtimecatalog

// HostStrategy registers the current detector/delivery/review combination and
// its bounded launch-options builder. It contains no Provider implementation facts.
type HostStrategy struct {
	Delivery     string
	Review       string
	Options      string
	ContentKinds []string
}

const (
	OptionsNone       = "NONE"
	OptionsEmulator   = "EMULATOR_CONTENT"
	OptionsRPGRestore = "RPG_RESTORE"
	OptionsONS        = "ONS_SCRIPT"
	OptionsKiriKiri   = "KIRIKIRI_STARTUP"
)

var hostStrategies = map[string]HostStrategy{
	"EMULATORJS_SINGLE_FILE":  {"EMULATORJS_CONTENT", "NONE", OptionsEmulator, []string{"SINGLE_FILE"}},
	"EMULATORJS_DISC_CONTENT": {"EMULATORJS_CONTENT", "NONE", OptionsEmulator, []string{"MULTI_DISC", "SINGLE_FILE"}},
	"ARCADE_ROM_SET":          {"EMULATORJS_CONTENT", "NONE", OptionsEmulator, []string{"SINGLE_FILE"}},
	"DOS_BUNDLE":              {"EMULATORJS_CONTENT", "NONE", OptionsEmulator, []string{"DOS_BUNDLE"}},
	"WASM4_CART":              {"ROM_BLOB", "NONE", OptionsNone, []string{"SINGLE_FILE"}},
	"RPG2000":                 rpgStrategy("FILE_TREE_PROJECT"),
	"RPG2003":                 rpgStrategy("FILE_TREE_PROJECT"),
	"RPGXP":                   rpgStrategy("SEEKABLE_PROJECT_ARCHIVE"),
	"RPGVX":                   rpgStrategy("SEEKABLE_PROJECT_ARCHIVE"),
	"RPGVXACE":                rpgStrategy("SEEKABLE_PROJECT_ARCHIVE"),
	"RPGMV":                   rpgStrategy("ISOLATED_WEB_PROJECT"),
	"RPGMZ":                   rpgStrategy("ISOLATED_WEB_PROJECT"),
	"ONS_PROJECT":             {"FILE_TREE_PROJECT", "NONE", OptionsONS, []string{"ONS_PROJECT"}},
	"KIRIKIRI_PROJECT":        {"FILE_TREE_PROJECT", "NONE", OptionsKiriKiri, []string{"KIRIKIRI_PROJECT"}},
	"BUTTERSCOTCH_PROJECT":    {"FILE_TREE_PROJECT", "NONE", OptionsNone, []string{"BUTTERSCOTCH_PROJECT"}},
	"TYRANOSCRIPT_PROJECT":    {"ISOLATED_WEB_PROJECT", "NONE", OptionsNone, []string{"TYRANOSCRIPT_PROJECT"}},
}

func Strategy(detector string) (HostStrategy, bool) {
	strategy, ok := hostStrategies[detector]
	strategy.ContentKinds = append([]string(nil), strategy.ContentKinds...)
	return strategy, ok
}

func validStrategy(binding Binding) bool {
	strategy, registered := Strategy(binding.DetectorProfile)
	if !registered {
		return false
	}
	for _, kind := range binding.AcceptedContentKinds {
		if !contains(strategy.ContentKinds, kind) {
			return false
		}
	}
	return true
}

func ValidPackLayout(layout, generation string) bool {
	switch layout {
	case "easy-rtp-layout-v1":
		return generation == "RPG2000" || generation == "RPG2003"
	case "mkxpz-v1":
		return generation == "RPGXP" || generation == "RPGVX" || generation == "RPGVXACE"
	default:
		return false
	}
}

func rpgStrategy(delivery string) HostStrategy {
	return HostStrategy{
		Delivery: delivery, Review: "RPG_RUNTIME_VALIDATION",
		Options: OptionsRPGRestore, ContentKinds: []string{"RPG_MAKER_PROJECT"},
	}
}
