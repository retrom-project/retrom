package arcadedat

import (
	"context"
	"strings"
	"testing"
)

func TestParserAllowsSafeDoctypeWithoutResolvingIt(t *testing.T) {
	t.Parallel()
	document := `<?xml version="1.0"?><!DOCTYPE datafile PUBLIC "-//TEST//DTD" "https://invalid.example/test.dtd"><datafile><game name="ok"><rom name="one.bin" size="1" crc="12345678"/></game></datafile>`
	stats, err := Parse(context.Background(), strings.NewReader(document), "fbneo")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if stats.MachineCount != 1 || stats.ROMEntryCount != 1 {
		t.Fatalf("stats = %+v", stats)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Machines) != 3 || catalog.Machines[0].Classification != "EXPLICIT_BIOS" ||
		catalog.Machines[0].ROMs[0].CRC32 != "aabbccdd" ||
		catalog.Machines[1].ROMOf != "base" {
		t.Fatalf("catalog = %#v", catalog)
	}
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
		if err != nil {
			t.Fatalf("Parse(%s): %v", coreID, err)
		}
		if stats.MachineCount != 1 || stats.ROMEntryCount != 1 {
			t.Fatalf("Parse(%s) stats = %#v", coreID, stats)
		}
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
