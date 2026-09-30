//go:build integration

package libraryimport

import "testing"

func TestServerImportResultProjectsCurrentBIOS(t *testing.T) {
	t.Parallel()
	fixture := newDeduplicateFixture(t)
	fixture.execute(t, `UPDATE bios_requirements SET requirement_mode='REQUIRED' WHERE core_id='mgba' AND logical_name='gba_bios.bin'`)
	created := fixture.create(t, "server-facts", "Retrom owned server result fixture", 1)
	result, err := fixture.service.serverImportResult(fixture.ctx, created.Created)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("server results=%+v", result)
	}
	item := result.Items[0]
	if item.ValidationStatus != "BLOCKED" || item.CompatibilityCode != "LAUNCH_BIOS_MISSING" || item.CoreID != "mgba" || item.CoreName == "" || item.DependencySnapshotJSON == "" {
		t.Fatalf("current server facts=%+v", item)
	}
}
