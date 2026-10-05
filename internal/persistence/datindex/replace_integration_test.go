//go:build integration

package datindex_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/format/arcadedat"
	"retrom/internal/persistence/datindex"
	"retrom/internal/testsupport"
	"retrom/internal/testsupport/testpostgres"
)

func TestReplaceBoundsRoundTripsAndPreservesAtomicCatalog(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	target, err := testsupport.LookupRuntimeTarget(t.Context(), database.SQL, "mame2003")
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO dat_versions(
id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,parse_status,
is_active,version,created_at_ms,updated_at_ms)
VALUES('batch-dat','mame2003',?,?,'test.dat',?,'test','PENDING',0,1,0,0)`,
		target.ProviderID, target.TargetID, strings.Repeat("f", 64))
	if err != nil {
		t.Fatal(err)
	}
	catalog := batchCatalog(1100)
	before := database.SQL.Stats().SQLCalls
	replace := func(input arcadedat.Catalog) error {
		return dbapi.InTransaction(t.Context(), database.SQL, nil, func(tx dbapi.Tx) error {
			return datindex.Replace(t.Context(), tx, "batch-dat", input)
		})
	}
	if err := replace(catalog); err != nil {
		t.Fatal(err)
	}
	// Thousands of DAT records must not require thousands of network round trips.
	if calls := database.SQL.Stats().SQLCalls - before; calls > 32 {
		t.Errorf("catalog replacement made %d SQL calls for 4400 records", calls)
	}
	assertCatalogCounts(t, database.SQL, 1100)
	assertCatalogFields(t, database.SQL)
	catalog.Machines[0].Description = "uncommitted"
	catalog.Machines[1099].ROMs[0].SizeBytes = -1
	if err := replace(catalog); !testpostgres.HasCode(err, "23514") {
		t.Fatalf("expected constraint failure in a later batch, got %v", err)
	}
	assertCatalogCounts(t, database.SQL, 1100)
	assertCatalogFields(t, database.SQL)
	if err := replace(arcadedat.Catalog{}); err != nil {
		t.Fatal(err)
	}
	assertCatalogCounts(t, database.SQL, 0)
}

func batchCatalog(count int) arcadedat.Catalog {
	catalog := arcadedat.Catalog{Machines: make([]arcadedat.Machine, count)}
	for index := range catalog.Machines {
		catalog.Machines[index] = arcadedat.Machine{
			Name: fmt.Sprintf("game-'%04d", index), Description: "目录 ? '$1'", Year: "1990",
			Manufacturer: "fixture", ExplicitBIOS: true, Classification: "EXPLICIT_BIOS",
			BIOSSets: []arcadedat.BIOSSet{{Name: "default", Description: "system", Default: true}},
			ROMs: []arcadedat.ROMEntry{{
				Ordinal: 0, Name: "main.bin", SizeBytes: 123,
				CRC32: "12345678", Status: "GOOD", BIOSName: "default",
			}},
			Disks: []arcadedat.DiskEntry{{Ordinal: 0, Name: "disk", Status: "NODUMP"}},
		}
	}
	return catalog
}

func assertCatalogCounts(t *testing.T, database dbapi.DB, expected int) {
	t.Helper()
	for _, table := range []string{"dat_machines", "dat_bios_sets", "dat_rom_entries", "dat_disk_entries"} {
		var count int
		if err := dbapi.QueryRowContext(t.Context(), database,
			"SELECT count(*) FROM "+table+" WHERE dat_version_id='batch-dat'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatalf("%s count = %d, want %d", table, count, expected)
		}
	}
}

func assertCatalogFields(t *testing.T, database dbapi.DB) {
	t.Helper()
	var valid int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*)
FROM dat_machines m JOIN dat_bios_sets b USING(dat_version_id,machine_name)
JOIN dat_rom_entries r USING(dat_version_id,machine_name)
JOIN dat_disk_entries d USING(dat_version_id,machine_name)
WHERE m.dat_version_id='batch-dat' AND m.description=? AND m.year='1990'
AND m.manufacturer='fixture' AND m.cloneof IS NULL AND m.romof IS NULL
AND m.is_explicit_bios=1 AND m.classification='EXPLICIT_BIOS'
AND b.bios_name='default' AND b.description='system' AND b.is_default=1
AND r.ordinal=0 AND r.name='main.bin' AND r.size_bytes=123 AND r.crc32='12345678'
AND r.sha1 IS NULL AND r.status='GOOD' AND r.merge_name IS NULL AND r.bios_name='default'
AND d.ordinal=0 AND d.name='disk' AND d.sha1 IS NULL AND d.status='NODUMP'`, "目录 ? '$1'").Scan(&valid)
	if err != nil || valid != 1100 {
		t.Fatalf("catalog fields changed: matched=%d error=%v", valid, err)
	}
}
