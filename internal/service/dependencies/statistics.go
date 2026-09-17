package dependencies

import (
	"retrom/internal/capability/format/arcadedat"
	model "retrom/internal/model/dependencies"
)

func catalogFromStats(stats model.CatalogStats) arcadedat.Catalog {
	return arcadedat.Catalog{Stats: arcadedat.Stats{
		MachineCount:                 int(stats.MachineCount),
		ROMEntryCount:                int(stats.ROMEntryCount),
		DiskEntryCount:               int(stats.DiskEntryCount),
		BIOSSetCount:                 int(stats.BIOSSetCount),
		DefaultBIOSSetCount:          int(stats.DefaultBIOSSetCount),
		ExplicitBIOSMachineCount:     int(stats.ExplicitBIOSMachineCount),
		BaseDependencyTargetCount:    int(stats.BaseDependencyTargetCount),
		UnresolvedCloneofTargetCount: int(stats.UnresolvedCloneofCount),
		UnresolvedRomofTargetCount:   int(stats.UnresolvedRomofCount),
	}}
}

func statsMatch(actual arcadedat.Stats, expected model.CatalogStats) bool {
	return int64(actual.MachineCount) == expected.MachineCount && int64(actual.ROMEntryCount) == expected.ROMEntryCount &&
		int64(actual.DiskEntryCount) == expected.DiskEntryCount &&
		int64(actual.BIOSSetCount) == expected.BIOSSetCount &&
		int64(actual.DefaultBIOSSetCount) == expected.DefaultBIOSSetCount &&
		int64(actual.ExplicitBIOSMachineCount) == expected.ExplicitBIOSMachineCount &&
		int64(actual.BaseDependencyTargetCount) == expected.BaseDependencyTargetCount &&
		int64(actual.UnresolvedCloneofTargetCount) == expected.UnresolvedCloneofCount &&
		int64(actual.UnresolvedRomofTargetCount) == expected.UnresolvedRomofCount
}
