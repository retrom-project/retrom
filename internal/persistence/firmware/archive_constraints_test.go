package firmware

import (
	"database/sql"
	"testing"

	"retrom/internal/importing"
	firmwareservice "retrom/internal/service/firmware"
)

func TestInvalidArchiveFactsAreNotSilentlyIgnored(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := database.ExecContext(t.Context(), `CREATE TABLE archive_entries(
archive_blob_id TEXT,ordinal INTEGER,original_relative_path TEXT,normalized_path TEXT,ascii_casefold_path TEXT,
archive_format TEXT,compression_profile TEXT,uncompressed_size_bytes INTEGER CHECK(uncompressed_size_bytes>=0),
crc32 TEXT,md5 TEXT,sha1 TEXT,sha256 TEXT,materialized_blob_id TEXT,created_at_ms INTEGER,
PRIMARY KEY(archive_blob_id,ordinal))`); err != nil {
		t.Fatal(err)
	}
	err = New(database).WithWrite(t.Context(), func(scope firmwareservice.WriteScope) error {
		return scope.Archives.Put(t.Context(), "blob", []importing.ArchiveEntry{{Ordinal: 0, Size: -1}}, 1)
	})
	if err == nil {
		t.Fatal("invalid archive facts were accepted without a record")
	}
}
