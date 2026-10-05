//go:build integration

package libraryimport

import (
	"errors"
	"testing"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
	dbpostgres "retrom/internal/database/postgres"
)

func TestOwnedSourceRechecksPreparedInputsAndExecutionBeforeWriting(t *testing.T) {
	cases := map[string]string{
		"source version":  `UPDATE source_import_items SET version=version+1 WHERE id='018fbe68-0000-7000-8000-000000000021'`,
		"worker identity": `UPDATE jobs SET worker_id='new-worker' WHERE id='owner-work'`,
		"execution":       `UPDATE jobs SET execution_no=execution_no+1 WHERE id='owner-work'`,
		"target version": `UPDATE source_import_collections SET target_platform_instance_version=target_platform_instance_version+1
WHERE id='owner-collection'`,
		"copied blob": `UPDATE source_import_item_files SET file_record=NULL WHERE item_id='018fbe68-0000-7000-8000-000000000021'`,
		"primary path": `UPDATE source_import_item_files SET relative_path='different.gba' WHERE
item_id='018fbe68-0000-7000-8000-000000000021'`,
	}
	for name, statement := range cases {
		t.Run(name, func(t *testing.T) {
			fixture, request := ownedSourceFixture(t)
			var databaseName string
			if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT current_database()`).Scan(&databaseName); err != nil {
				t.Fatal(err)
			}
			intercepted := dbpostgres.OpenConnector(sourceFaultConnector{
				path: testpostgres.ForDatabase(t, databaseName),
				beforeCreation: func() error {
					_, err := fixture.database.ExecContext(fixture.ctx,
						statement)
					return err
				},
			}, dbpostgres.Options{})
			intercepted.SetMaxOpenConns(1)
			t.Cleanup(func() {
				if err := intercepted.Close(); err != nil {
					t.Error(err)
				}
			})
			fixture.service = newTestImporter(t, intercepted, fixture.service.blobs, testImportOptions{Now: fixture.service.now, MultiDiscEnabled: fixture.service.multiDiscImportEnabled})
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			if !errors.Is(err, ErrVersionConflict) || result.Created.ImportJobID != "" || result.Items != nil {
				t.Fatalf("%s changed during prepare: %#v %v", name, result, err)
			}
			var imports, linked int
			if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT (SELECT count(*) FROM import_jobs),(SELECT count(*) FROM source_import_items WHERE
library_import_item_id IS NOT NULL)`).Scan(&imports, &linked); err != nil {
				t.Fatal(err)
			}
			if imports != 0 || linked != 0 {
				t.Fatalf("stale preparation committed: imports=%d linked=%d", imports, linked)
			}
		})
	}
}
