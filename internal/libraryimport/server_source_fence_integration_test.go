//go:build integration

package libraryimport

import (
	"database/sql"
	"errors"
	"testing"
)

func TestOwnedSourceRechecksPreparedInputsAndExecutionBeforeWriting(t *testing.T) {
	cases := map[string]string{
		"source version":  `UPDATE pegasus_import_items SET version=version+1 WHERE id='unlinked-source'`,
		"worker identity": `UPDATE jobs SET worker_id='new-worker' WHERE id='owner-work'`,
		"execution":       `UPDATE jobs SET execution_no=execution_no+1 WHERE id='owner-work'`,
		"target version":  `UPDATE pegasus_import_collections SET target_platform_instance_version=target_platform_instance_version+1 WHERE id='owner-collection'`,
		"copied blob":     `UPDATE pegasus_import_item_files SET blob_id=NULL WHERE item_id='unlinked-source'`,
		"primary path":    `UPDATE pegasus_import_item_files SET relative_path='different.gba' WHERE item_id='unlinked-source'`,
	}
	for name, statement := range cases {
		t.Run(name, func(t *testing.T) {
			fixture, request := ownedSourceFixture(t)
			var ordinal int
			var schema, path string
			if err := fixture.database.QueryRowContext(fixture.ctx, `PRAGMA database_list`).Scan(&ordinal, &schema, &path); err != nil {
				t.Fatal(err)
			}
			intercepted := sql.OpenDB(sourceFaultConnector{path: path, beforeCreation: func() error { _, err := fixture.database.ExecContext(fixture.ctx, statement); return err }})
			intercepted.SetMaxOpenConns(1)
			t.Cleanup(func() {
				if err := intercepted.Close(); err != nil {
					t.Error(err)
				}
			})
			fixture.service.database = intercepted
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			if !errors.Is(err, ErrVersionConflict) || result.Created.ImportJobID != "" || result.Items != nil {
				t.Fatalf("%s changed during prepare: %#v %v", name, result, err)
			}
			var imports, linked int
			if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT (SELECT count(*) FROM import_jobs),(SELECT count(*) FROM pegasus_import_items WHERE library_import_item_id IS NOT NULL)`).Scan(&imports, &linked); err != nil {
				t.Fatal(err)
			}
			if imports != 0 || linked != 0 {
				t.Fatalf("stale preparation committed: imports=%d linked=%d", imports, linked)
			}
		})
	}
}
