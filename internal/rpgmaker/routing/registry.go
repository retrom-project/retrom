package routing

import (
	"errors"
	"fmt"
	"sort"

	"retrom/internal/rpgmaker/detector"
)

var ErrUnavailable = errors.New("RPG_RUNTIME_ROUTE_UNAVAILABLE")

type AdapterKind string

const (
	AdapterEasyRPG      AdapterKind = "EASYRPG_WEB"
	AdapterMkxp         AdapterKind = "MKXP_LIBRETRO_WEB"
	AdapterNativeWeb    AdapterKind = "NATIVE_WEB"
	FamilyRPGMaker                  = "RPGMAKER"
	PayloadRuntimeState             = "RUNTIME_STATE"
	PayloadNativeBundle             = "NATIVE_SAVE_BUNDLE_V1"
)

type Entry struct {
	CoreID                 string
	Generation             detector.Generation
	RouteKey               string
	RuntimeFamily          string
	AdapterKind            AdapterKind
	AdapterID              string
	AdapterABI             string
	RuntimeVersion         string
	EngineMode             string
	RGSSVersion            int
	RequiresThreads        bool
	SavePayloadKind        string
	SaveMaxBytes           int64
	SelectedForNewBindings bool
}

var registry = []Entry{
	{
		CoreID: "rpgmaker_2000", Generation: detector.RPG2000, RouteKey: "RPG2000_EASYRPG",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterEasyRPG, AdapterID: "easyrpg-web",
		AdapterABI:     "easyrpg-save",
		RuntimeVersion: "v0.7.4", EngineMode: "rpg2k", SavePayloadKind: PayloadNativeBundle,
		SaveMaxBytes: 64 << 20, SelectedForNewBindings: true,
	},
	{
		CoreID: "rpgmaker_2003", Generation: detector.RPG2003, RouteKey: "RPG2003_EASYRPG",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterEasyRPG, AdapterID: "easyrpg-web",
		AdapterABI:     "easyrpg-save",
		RuntimeVersion: "v0.7.4", EngineMode: "rpg2k3", SavePayloadKind: PayloadNativeBundle,
		SaveMaxBytes: 64 << 20, SelectedForNewBindings: true,
	},
	{
		CoreID: "rpgmaker_xp", Generation: detector.RPGXP, RouteKey: "RPGXP_MKXP",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterMkxp, AdapterID: "mkxp-libretro-web",
		AdapterABI:     "mkxp-state-compact",
		RuntimeVersion: "v0.7.4", RGSSVersion: 1, RequiresThreads: true,
		SavePayloadKind: PayloadRuntimeState, SaveMaxBytes: 256 << 20, SelectedForNewBindings: true,
	},
	{
		CoreID: "rpgmaker_vx", Generation: detector.RPGVX, RouteKey: "RPGVX_MKXP",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterMkxp, AdapterID: "mkxp-libretro-web",
		AdapterABI:     "mkxp-state-compact",
		RuntimeVersion: "v0.7.4", RGSSVersion: 2, RequiresThreads: true,
		SavePayloadKind: PayloadRuntimeState, SaveMaxBytes: 256 << 20, SelectedForNewBindings: true,
	},
	{
		CoreID: "rpgmaker_vx_ace", Generation: detector.RPGVXAce, RouteKey: "RPGVXACE_MKXP",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterMkxp, AdapterID: "mkxp-libretro-web",
		AdapterABI:     "mkxp-state-compact",
		RuntimeVersion: "v0.7.4", RGSSVersion: 3, RequiresThreads: true,
		SavePayloadKind: PayloadRuntimeState, SaveMaxBytes: 256 << 20, SelectedForNewBindings: true,
	},
	{
		CoreID: "rpgmaker_mv", Generation: detector.RPGMV, RouteKey: "RPGMV_NATIVE",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterNativeWeb, AdapterID: "native-web",
		AdapterABI:     "native-save",
		RuntimeVersion: "v0.7.4", SavePayloadKind: PayloadNativeBundle,
		SaveMaxBytes: 64 << 20, SelectedForNewBindings: true,
	},
	{
		CoreID: "rpgmaker_mz", Generation: detector.RPGMZ, RouteKey: "RPGMZ_NATIVE",
		RuntimeFamily: FamilyRPGMaker, AdapterKind: AdapterNativeWeb, AdapterID: "native-web",
		AdapterABI:     "native-save",
		RuntimeVersion: "v0.7.4", SavePayloadKind: PayloadNativeBundle,
		SaveMaxBytes: 64 << 20, SelectedForNewBindings: true,
	},
}

func Entries() []Entry {
	return append([]Entry(nil), registry...)
}

func Current(coreID string, generation detector.Generation) (Entry, error) {
	var result Entry
	matches := 0
	for _, entry := range registry {
		if entry.CoreID == coreID && entry.Generation == generation && entry.SelectedForNewBindings {
			result = entry
			matches++
		}
	}
	if matches != 1 {
		return Entry{}, ErrUnavailable
	}
	return result, nil
}

func ByRoute(coreID, routeKey string) (Entry, error) {
	for _, entry := range registry {
		if entry.CoreID == coreID && entry.RouteKey == routeKey {
			return entry, nil
		}
	}
	return Entry{}, ErrUnavailable
}

func Validate() error {
	byRoute := make(map[string]struct{}, len(registry))
	currentByCore := make(map[string]int, len(registry))
	ordered := append([]Entry(nil), registry...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].CoreID < ordered[right].CoreID })
	for _, entry := range ordered {
		if err := validateEntry(entry); err != nil {
			return err
		}
		if _, duplicate := byRoute[entry.RouteKey]; duplicate {
			return fmt.Errorf("%w: duplicate route %s", ErrUnavailable, entry.RouteKey)
		}
		byRoute[entry.RouteKey] = struct{}{}
		if entry.SelectedForNewBindings {
			currentByCore[entry.CoreID]++
		}
	}
	for coreID := range supportedCoreIDs() {
		if currentByCore[coreID] != 1 {
			return fmt.Errorf("%w: core %s has %d current routes", ErrUnavailable, coreID, currentByCore[coreID])
		}
	}
	return nil
}

func validateEntry(entry Entry) error {
	if !validEntryIdentity(entry) {
		return fmt.Errorf("%w: invalid route %s", ErrUnavailable, entry.RouteKey)
	}
	if !validAdapterEntry(entry) {
		return fmt.Errorf("%w: invalid adapter route %s", ErrUnavailable, entry.RouteKey)
	}
	return nil
}

func validEntryIdentity(entry Entry) bool {
	expected, err := detector.GenerationForCore(entry.CoreID)
	return err == nil && expected == entry.Generation && entry.RouteKey != "" &&
		entry.RuntimeFamily == FamilyRPGMaker && entry.AdapterID != "" && entry.AdapterABI != "" &&
		entry.RuntimeVersion != "" && entry.SaveMaxBytes > 0
}

func validAdapterEntry(entry Entry) bool {
	switch entry.AdapterKind {
	case AdapterEasyRPG:
		return validEasyRPGEntry(entry)
	case AdapterMkxp:
		return validMKXPEntry(entry)
	case AdapterNativeWeb:
		return validNativeEntry(entry)
	default:
		return false
	}
}

func validEasyRPGEntry(entry Entry) bool {
	return (entry.EngineMode == "rpg2k" || entry.EngineMode == "rpg2k3") && entry.RGSSVersion == 0 &&
		!entry.RequiresThreads && entry.SavePayloadKind == PayloadNativeBundle
}

func validMKXPEntry(entry Entry) bool {
	return entry.EngineMode == "" && entry.RGSSVersion >= 1 && entry.RGSSVersion <= 3 &&
		entry.RequiresThreads && entry.SavePayloadKind == PayloadRuntimeState
}

func validNativeEntry(entry Entry) bool {
	return entry.EngineMode == "" && entry.RGSSVersion == 0 && !entry.RequiresThreads &&
		entry.SavePayloadKind == PayloadNativeBundle
}

func supportedCoreIDs() map[string]struct{} {
	return map[string]struct{}{
		"rpgmaker_2000": {}, "rpgmaker_2003": {}, "rpgmaker_xp": {}, "rpgmaker_vx": {},
		"rpgmaker_vx_ace": {}, "rpgmaker_mv": {}, "rpgmaker_mz": {},
	}
}
