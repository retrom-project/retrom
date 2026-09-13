package emulationstationimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/arcadedat"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/datindex"
	"retrom/internal/testsupport"
)

func companionFixture(t *testing.T) (lifecycleFixture, work, executionItem) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	var core, targetID string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT default_core_id,id FROM platform_instances WHERE platform_id='arcade' ORDER BY sort_order,id LIMIT 1`).Scan(

		&core,

		&targetID,
	); err != nil {
		t.Fatalf("arcade catalog: %v", err)
	}
	root, datFile := companionPublicSource(t, core)
	for _, file := range []string{"pacman.zip", "puckman.zip", "retrombios.zip"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		writeScanFile(t, fixture.source, "companions/"+file, data)
	}
	writeScanFile(
		t,
		fixture.source,
		"companions/gamelist.xml",
		[]byte(
			`<gameList><game><path>pacman.zip</path><name>Child</name></game><game><path>puckman.zip</path><name>Parent</name></game><game><path>retrombios.zip</path><name>Dependency</name></game></gameList>`,
		),
	)
	seedCompanionDAT(t, fixture, root, core, datFile)
	scanned := fixture.createAndScan(t)
	collections, err := fixture.service.Collections(fixture.context, scanned.ID, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	target := targetID
	mappings := make([]Mapping, 0, len(collections))
	for _, collection := range collections {
		mapping := Mapping{CollectionID: collection.ID, Action: "SKIP", TagIDs: []string{}}
		if collection.RelativeDirectory == "companions" {
			mapping.Action = "IMPORT"
			mapping.PlatformInstanceID = target
		}
		mappings = append(mappings, mapping)
	}
	mapped, err := fixture.service.UpdateMappings(fixture.context, scanned.ID, scanned.Version, mappings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	unit, found, err := fixture.service.claim(fixture.context)
	if err != nil || !found {
		t.Fatalf("claim=%v error=%v", found, err)
	}
	item, found, err := fixture.service.nextItem(fixture.context, unit)
	if err != nil || !found {
		t.Fatalf("item=%v error=%v", found, err)
	}
	if len(item.Files) != 1 || item.Files[0].Path != "companions/pacman.zip" {
		t.Fatalf("primary=%#v", item)
	}
	return fixture, unit, item
}

func seedCompanionDAT(t *testing.T, fixture lifecycleFixture, root, core, datFile string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, datFile))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := arcadedat.ParseCatalog(fixture.context, bytes.NewReader(contents), core)
	if err != nil {
		t.Fatal(err)
	}
	target, err := testsupport.LookupRuntimeTarget(fixture.context, fixture.database, core)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	tx, err := fixture.database.BeginTx(fixture.context, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := tx.ExecContext(
		fixture.context,
		`UPDATE dat_versions SET is_active=0 WHERE provider_id=? AND target_id=?`,
		target.ProviderID,
		target.TargetID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(
		fixture.context,
		`INSERT INTO dat_versions(id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms) VALUES('es-companion-dat',?,?,?,?,?,'fixture','READY',1,?,?,?,?,?,?,?,?,1,1,1,1,1)`,
		core,
		target.ProviderID,
		target.TargetID,
		filepath.ToSlash(filepath.Join(root, datFile)),
		hex.EncodeToString(digest[:]),
		catalog.Stats.MachineCount,
		catalog.Stats.ROMEntryCount,
		catalog.Stats.DiskEntryCount,
		catalog.Stats.BIOSSetCount,
		catalog.Stats.DefaultBIOSSetCount,
		catalog.Stats.ExplicitBIOSMachineCount,
		catalog.Stats.BaseDependencyTargetCount,
		catalog.Stats.UnresolvedCloneofTargetCount+catalog.Stats.UnresolvedRomofTargetCount,
	); err != nil {
		t.Fatal(err)
	}
	if err := datindex.Replace(fixture.context, tx, "es-companion-dat", catalog); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func companionPublicSource(t *testing.T, core string) (string, string) {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "public-roms", "arcade-smoke")
	datFile := "mame2003-smoke.xml"
	switch core {
	case "mame2003":
	case "mame2003_plus":
		root = filepath.Join(root, core)
		datFile = "mame2003-plus-smoke.xml"
	case "fbneo":
		root = filepath.Join(root, core)
		datFile = "fbneo-smoke.dat"
	default:
		t.Fatalf("no public companion fixture for catalog core %s", core)
	}
	return root, datFile
}
