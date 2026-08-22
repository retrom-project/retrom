package arcadedat

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestParserAllowsSafeDoctypeWithoutResolvingIt(t *testing.T) {
	t.Parallel()
	document := `<?xml version="1.0"?><!DOCTYPE datafile PUBLIC "-//TEST//DTD" "https://invalid.example/test.dtd"><datafile><game name="ok"><rom name="one.bin" size="1" crc="12345678"/></game></datafile>`
	stats, err := Parse(context.Background(), strings.NewReader(document), "fbneo")
	testassert.Falsef(t, err != nil, "Parse() error = %v", err)
	testassert.Falsef(t, testassert.Any(func() bool { return stats.MachineCount != 1 }, func() bool { return stats.ROMEntryCount != 1 }), "stats = %+v", stats)
}

func TestParserRejectsEntityDirective(t *testing.T) {
	t.Parallel()
	document := `<!DOCTYPE datafile [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><datafile/>`
	if _, err := Parse(context.Background(), strings.NewReader(document), "fbneo"); err == nil {
		t.Fatal("entity directive was accepted")
	}
}

func TestParseCatalogMaterializesMachineRelationshipsAndEntries(t *testing.T) {
	t.Parallel()
	document := `<?xml version="1.0"?><mame><machine name="base" isbios="yes"><description>Base</description><year>1990</year><manufacturer>Maker</manufacturer><biosset name="world" description="World" default="yes"/><rom name="base.bin" size="2" crc="AABBCCDD" sha1="0123456789012345678901234567890123456789" bios="world"/><disk name="base" sha1="1123456789012345678901234567890123456789"/></machine><machine name="child" cloneof="parent" romof="base"><description>Child</description><rom name="child.bin" size="1" crc="12345678" merge="parent.bin"/></machine><machine name="parent"><description>Parent</description><rom name="parent.bin" size="1" crc="87654321"/></machine></mame>`
	catalog, err := ParseCatalog(context.Background(), strings.NewReader(document), "mame2003")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return len(catalog.Machines) != 3 }, func() bool { return catalog.Machines[0].Classification != "EXPLICIT_BIOS" }, func() bool { return catalog.Machines[0].ROMs[0].CRC32 != "aabbccdd" }, func() bool { return catalog.Machines[1].ROMOf != "base" }), "catalog = %#v", catalog)
}

func TestParseCatalogRejectsAmbiguousBIOS(t *testing.T) {
	t.Parallel()
	document := `<mame><machine name="bad"><biosset name="a"/><rom name="x" size="1" crc="12345678" bios="a"/></machine></mame>`
	if _, err := ParseCatalog(context.Background(), strings.NewReader(document), "mame2003"); err == nil {
		t.Fatalf("invalid catalog accepted: %s", document)
	}
}

func TestParseFBA2012UsesLogiqxDatafileFamily(t *testing.T) {
	t.Parallel()
	document := `<datafile><machine name="1941"><description>1941</description><rom name="41em_30.11f" size="131072" crc="9deb1e75"/></machine></datafile>`
	for _, coreID := range []string{"fbalpha2012_cps1", "fbalpha2012_cps2"} {
		stats, err := Parse(context.Background(), strings.NewReader(document), coreID)
		testassert.Falsef(t, err != nil, "Parse(%s): %v", coreID, err)
		testassert.Falsef(t, testassert.Any(func() bool { return stats.MachineCount != 1 }, func() bool { return stats.ROMEntryCount != 1 }), "Parse(%s) stats = %#v", coreID, stats)
	}
}

func TestParseRejectsRootFromAnotherDATFamily(t *testing.T) {
	t.Parallel()
	if _, err := Parse(context.Background(), strings.NewReader(`<mame><machine name="1941"/></mame>`), "fbalpha2012_cps1"); err == nil {
		t.Fatal("expected FBA2012 to reject a MAME listxml root")
	}
	if _, err := Parse(context.Background(), strings.NewReader(`<datafile><machine name="1941"/></datafile>`), "mame2003"); err == nil {
		t.Fatal("expected MAME 2003 to reject a Logiqx datafile root")
	}
}

func TestPublicMAME2003SmokeDATMaterializesExecutableDependencyContract(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	source, err := os.Open(filepath.Join(
		repositoryRoot,
		"testdata",
		"public-roms",
		"arcade-smoke",
		"mame2003-smoke.xml",
	))
	testassert.False(t, err != nil, err)
	defer func() {
		if err := source.Close(); err != nil {
			t.Errorf("close public Arcade smoke DAT: %v", err)
		}
	}()
	catalog, err := ParseCatalog(context.Background(), source, "mame2003")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return catalog.Stats.MachineCount != 3 }, func() bool { return catalog.Stats.ROMEntryCount != 17 }, func() bool { return catalog.Stats.ROMEntryWithMergeCount != 6 }, func() bool { return catalog.Stats.CloneofRelationCount != 1 }, func() bool { return catalog.Stats.RomofRelationCount != 2 }, func() bool { return catalog.Stats.ExplicitBIOSMachineCount != 1 }, func() bool { return catalog.Stats.BaseDependencyTargetCount != 1 }), "public Arcade smoke stats = %#v", catalog.Stats)
	machines := make(map[string]Machine, len(catalog.Machines))
	for _, machine := range catalog.Machines {
		machines[machine.Name] = machine
	}
	child, parent, bios := machines["pacman"], machines["puckman"], machines["retrombios"]
	testassert.Falsef(t, testassert.Any(func() bool { return child.CloneOf != "puckman" }, func() bool { return child.ROMOf != "retrombios" }, func() bool { return len(child.ROMs) != 10 }, func() bool { return parent.ROMOf != "retrombios" }, func() bool { return len(parent.ROMs) != 6 }, func() bool { return bios.Classification != "EXPLICIT_BIOS" }, func() bool { return len(bios.ROMs) != 1 }), "public Arcade smoke catalog = %#v", catalog.Machines)
}

func TestPublicFBNeoSmokeDATMaterializesExecutableDependencyContract(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	source, err := os.Open(filepath.Join(
		repositoryRoot,
		"testdata",
		"public-roms",
		"arcade-smoke",
		"fbneo",
		"fbneo-smoke.dat",
	))
	testassert.False(t, err != nil, err)
	defer func() {
		if err := source.Close(); err != nil {
			t.Errorf("close public FBNeo smoke DAT: %v", err)
		}
	}()
	catalog, err := ParseCatalog(context.Background(), source, "fbneo")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return catalog.Stats.MachineCount != 3 }, func() bool { return catalog.Stats.ROMEntryCount != 15 }, func() bool { return catalog.Stats.ROMEntryWithMergeCount != 4 }, func() bool { return catalog.Stats.CloneofRelationCount != 1 }, func() bool { return catalog.Stats.RomofRelationCount != 2 }, func() bool { return catalog.Stats.ExplicitBIOSMachineCount != 1 }, func() bool { return catalog.Stats.BaseDependencyTargetCount != 1 }), "public FBNeo smoke stats = %#v", catalog.Stats)
	machines := make(map[string]Machine, len(catalog.Machines))
	for _, machine := range catalog.Machines {
		machines[machine.Name] = machine
	}
	child, parent, bios := machines["pacman"], machines["puckman"], machines["retrombios"]
	testassert.Falsef(t, testassert.Any(func() bool { return child.CloneOf != "puckman" }, func() bool { return child.ROMOf != "retrombios" }, func() bool { return len(child.ROMs) != 10 }, func() bool { return parent.ROMOf != "retrombios" }, func() bool { return len(parent.ROMs) != 4 }, func() bool { return bios.Classification != "EXPLICIT_BIOS" }, func() bool { return len(bios.ROMs) != 1 }), "public FBNeo smoke catalog = %#v", catalog.Machines)
}
