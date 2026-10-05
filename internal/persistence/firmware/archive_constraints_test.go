package firmware

import (
	"testing"

	dbpostgres "retrom/internal/database/postgres"
	"retrom/internal/importing"
	firmwareservice "retrom/internal/service/firmware"
	"retrom/internal/testsupport/testpostgres"
)

func TestInvalidArchiveFactsAreNotSilentlyIgnored(t *testing.T) {
	database, err := dbpostgres.Open(testpostgres.DSN(t), dbpostgres.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := database.ExecContext(t.Context(), `CREATE TABLE archive_entries(
archive_file_record TEXT,ordinal BIGINT,original_relative_path TEXT,normalized_path TEXT,
ascii_casefold_path TEXT,
archive_format TEXT,compression_profile TEXT,uncompressed_size_bytes BIGINT
CHECK(uncompressed_size_bytes>=0),
crc32 TEXT,md5 TEXT,sha1 TEXT,sha256 TEXT,created_at_ms BIGINT,
PRIMARY KEY(archive_file_record,ordinal))`); err != nil {
		t.Fatal(err)
	}
	err = New(database).WithWrite(t.Context(), func(scope firmwareservice.WriteScope) error {
		return scope.Archives.Put(t.Context(), "blob", []importing.ArchiveEntry{{Ordinal: 0, Size: -1}}, 1)
	})
	if err == nil {
		t.Fatal("invalid archive facts were accepted without a record")
	}
}
