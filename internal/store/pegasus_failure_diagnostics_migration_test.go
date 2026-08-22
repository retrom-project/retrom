package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"retrom/internal/testassert"

	_ "modernc.org/sqlite"
)

func TestPegasusFailureDiagnosticsMigrationBackfillsArcadeSourceLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration-029.db"))
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(ctx, `
CREATE TABLE pegasus_import_collections(
 id TEXT PRIMARY KEY,mapping_action TEXT,target_platform_instance_id TEXT,target_platform_id TEXT
);
CREATE TABLE pegasus_import_items(
 id TEXT PRIMARY KEY,import_id TEXT,collection_id TEXT,discovery_state TEXT,execution_state TEXT,
 error_code TEXT,library_import_job_id TEXT,library_import_item_id TEXT
);
CREATE TABLE pegasus_import_item_files(
 item_id TEXT,ordinal INTEGER,relative_path TEXT
);
INSERT INTO pegasus_import_collections VALUES('collection','IMPORT','fbneo-platform','arcade');
INSERT INTO pegasus_import_items VALUES(
 'primary','import','collection','READY','COMMIT_FAILED','PEGASUS_LIBRARY_IMPORT_FAILED',NULL,NULL
);
INSERT INTO pegasus_import_item_files VALUES('primary',0,'1944j.zip');
`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 64; index++ {
		id := fmt.Sprintf("candidate-%02d", index)
		if _, err := database.ExecContext(
			ctx,
			`INSERT INTO pegasus_import_items VALUES(?,'import','collection','READY','PENDING',NULL,NULL,NULL)`,
			id,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(
			ctx,
			`INSERT INTO pegasus_import_item_files VALUES(?,0,?)`,
			id,
			fmt.Sprintf("candidate-%02d.zip", index),
		); err != nil {
			t.Fatal(err)
		}
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	migration, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "029_pegasus_failure_diagnostics.sql"))
	testassert.False(t, err != nil, err)
	if _, err := database.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var stage, operation, cause, relativePath, detail string
	var observed, allowed int64
	if err := database.QueryRowContext(ctx, `
SELECT json_extract(error_details_json,'$.stage'),json_extract(error_details_json,'$.operation'),
json_extract(error_details_json,'$.causeCode'),json_extract(error_details_json,'$.relativePath'),
json_extract(error_details_json,'$.observedFileCount'),json_extract(error_details_json,'$.allowedFileCount'),
json_extract(error_details_json,'$.technicalDetail')
FROM pegasus_import_items WHERE id='primary'
`).Scan(&stage, &operation, &cause, &relativePath, &observed, &allowed, &detail); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return stage != "LIBRARY_IMPORT" }, func() bool { return operation != "CREATE_SERVER_SOURCE" }, func() bool { return cause != "SOURCE_FILE_LIMIT_EXCEEDED" }, func() bool { return relativePath != "1944j.zip" }, func() bool { return observed != 65 }, func() bool { return allowed != 64 }, func() bool { return detail == "" }), "details = %s/%s/%s path=%s files=%d/%d detail=%q", stage, operation, cause, relativePath, observed, allowed, detail)
	if _, err := database.ExecContext(
		ctx, `UPDATE pegasus_import_items SET error_details_json='[]' WHERE id='primary'`,
	); err == nil {
		t.Fatal("non-object failure details must be rejected")
	}
}
