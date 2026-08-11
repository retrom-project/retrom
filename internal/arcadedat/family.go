package arcadedat

import "sort"

type Family string

const (
	FamilyLogiqxDatafile Family = "LOGIQX_DATAFILE"
	FamilyMAMEListXML    Family = "MAME_LISTXML"
)

var coreFamilies = map[string]Family{
	"fbneo":            FamilyLogiqxDatafile,
	"fbalpha2012_cps1": FamilyLogiqxDatafile,
	"fbalpha2012_cps2": FamilyLogiqxDatafile,
	"mame2003":         FamilyMAMEListXML,
	"mame2003_plus":    FamilyMAMEListXML,
}

func FamilyForCore(coreID string) (Family, bool) {
	family, exists := coreFamilies[coreID]
	return family, exists
}

func SupportsCore(coreID string) bool {
	_, exists := FamilyForCore(coreID)
	return exists
}

func CoreIDs() []string {
	coreIDs := make([]string, 0, len(coreFamilies))
	for coreID := range coreFamilies {
		coreIDs = append(coreIDs, coreID)
	}
	sort.Strings(coreIDs)
	return coreIDs
}
