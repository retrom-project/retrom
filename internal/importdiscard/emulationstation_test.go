package importdiscard

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/emulationstationimport"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/testsupport"
)

func (f *fixture) emulationStationSource(t *testing.T, file libraryimport.ServerSourceFile) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, file.RelativePath), bytes.Repeat([]byte{11}, int(file.SizeBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	xml := `<gameList><game><path>./` + file.RelativePath + `</path><name>Rejected</name></game></gameList>`
	if err := os.WriteFile(filepath.Join(dir, "gamelist.xml"), []byte(xml), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := emulationstationimport.New(f.db, f.blobs, f.importer, credentials, []serversource.Root{{ID: "games", Label: "Games", Path: dir}}, f.now)
	service.Start()
	t.Cleanup(service.Close)
	f.service.emulationstation = service
	created, err := service.Create(f.ctx, emulationstationimport.CreateRequest{RootID: "games"}, adminID)
	if err != nil {
		t.Fatal(err)
	}
	scanned := waitForES(t, f, service, created.ID, "AWAITING_MAPPING")
	collections, err := service.Collections(f.ctx, created.ID, "", "", 10)
	if err != nil || len(collections) != 1 {
		t.Fatalf("collections: %#v %v", collections, err)
	}
	mapped, err := service.UpdateMappings(f.ctx, created.ID, scanned.Version, []emulationstationimport.Mapping{{
		CollectionID: collections[0].ID, Action: "IMPORT", TagIDs: []string{}, PlatformInstanceID: testsupport.MustPlatformInstanceID(t, f.db, "nes/fceumm"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartImport(f.ctx, created.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	waitForES(t, f, service, created.ID, "COMPLETED", "PARTIAL_FAILURE")
	var itemID string
	if err := f.db.QueryRowContext(f.ctx, `SELECT id FROM emulationstation_import_items WHERE import_id=?`, created.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return created.ID, itemID
}

func waitForES(t *testing.T, f *fixture, service *emulationstationimport.Service, id string, states ...string) emulationstationimport.Summary {
	t.Helper()
	// The product clock is fixed; only wait for the worker to finish its deterministic filesystem work.
	for range 250 {
		summary, err := service.Get(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range states {
			if summary.State == state {
				return summary
			}
		}
		if summary.State == "FAILED" {
			t.Fatalf("source worker failed: %#v", summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("source worker did not finish within 2.5 seconds")
	return emulationstationimport.Summary{}
}
