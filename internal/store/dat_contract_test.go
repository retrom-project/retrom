package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestCurrentDATSchemaRejectsNonReleaseCatalogState(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { testassert.False(t, database.Close() != nil, "close database") })
	assertColumns(t, database.SQL, "dat_versions", "provider_id", "target_id", "builtin_relative_path",
		"sha256", "parser_version", "parse_status")
	columns := tableColumns(t, database.SQL, "dat_versions")
	for _, column := range []string{"source_kind", "blob_id", "base_dat_version_id", "compatibility_json"} {
		testassert.Falsef(t, columns[column], "legacy DAT column %s remains", column)
	}
	names := queryStrings(t, database.SQL, `SELECT name FROM sqlite_schema
WHERE type='table' AND (name LIKE 'dat_import%' OR name LIKE 'dat_diff%')`)
	testassert.Truef(t, len(names) == 0, "legacy DAT tables remain: %v", names)
	_, err = database.SQL.ExecContext(t.Context(), `UPDATE dat_versions SET source_kind='USER'`)
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "source_kind"),
		"user DAT source was accepted: %v", err)
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO dat_versions(
id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,parse_status,
is_active,version,created_at_ms,updated_at_ms)
VALUES('unbound-dat','undeclared-core','unknown-provider','unknown-target','test.dat',
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','current','PENDING',0,1,0,0)`)
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid runtime target snapshot"),
		"unbound DAT was accepted: %v", err)
	testassert.False(t, database.IntegrityCheck(t.Context()) != nil, "DAT schema integrity")
}
