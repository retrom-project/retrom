//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestOwnedESSourceRechecksFrozenInputsAfterPreparation(t *testing.T) {
	cases := map[string]string{
		"worker":         `UPDATE jobs SET worker_id='other-worker' WHERE id='es-owner-work'`,
		"attempt":        `UPDATE jobs SET attempt_count=2 WHERE id='es-owner-work'`,
		"execution":      `UPDATE jobs SET execution_no=2 WHERE id='es-owner-work'`,
		"source version": `UPDATE emulationstation_import_items SET version=version+1 WHERE id='es-owner-source'`,
		"root digest":    `UPDATE emulationstation_imports SET root_config_digest=replace(root_config_digest,'a','b') WHERE id='es-owner-plan'`,
		"year":           `UPDATE emulationstation_imports SET release_year_max=2034 WHERE id='es-owner-plan'`,
		"mapping":        `UPDATE emulationstation_imports SET mapping_version=mapping_version+1 WHERE id='es-owner-plan'`,
		"target":         `UPDATE emulationstation_import_collections SET target_platform_instance_version=2 WHERE id='es-owner-collection'`,
		"copied file":    `UPDATE emulationstation_import_item_files SET blob_id=NULL WHERE item_id='es-owner-source'`,
	}
	for name, statement := range cases {
		t.Run(name, func(t *testing.T) {
			fixture, request := ownedESSourceFixture(t)
			reads := 0
			fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(ctx context.Context, query string, _ []driver.NamedValue) error {
					if !strings.Contains(query, "SELECT source.id,source.import_id") {
						return nil
					}
					reads++
					if reads != 2 {
						return nil
					}
					_, err := fixture.database.ExecContext(ctx, statement)
					return err
				},
			})
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			if !errors.Is(err, ErrVersionConflict) || result.Created.ImportJobID != "" || result.Items != nil || reads != 2 {
				t.Fatalf("stale %s result=%+v reads=%d err=%v", name, result, reads, err)
			}
			assertNoOwnedESImport(t, fixture)
		})
	}
}

func assertNoOwnedESImport(t *testing.T, fixture deduplicateFixture) {
	t.Helper()
	var imports, items, drafts, bindings int
	err := fixture.database.QueryRowContext(fixture.ctx, `SELECT (SELECT count(*) FROM import_jobs),
 (SELECT count(*) FROM import_items),(SELECT count(*) FROM review_drafts),
 (SELECT count(*) FROM emulationstation_import_items WHERE library_import_item_id IS NOT NULL OR library_import_job_id IS NOT NULL)`).Scan(&imports, &items, &drafts, &bindings)
	if err != nil {
		t.Fatal(err)
	}
	if imports != 0 || items != 0 || drafts != 0 || bindings != 0 {
		t.Fatalf("partial creation imports=%d items=%d drafts=%d bindings=%d", imports, items, drafts, bindings)
	}
}
