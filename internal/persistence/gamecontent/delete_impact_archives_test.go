package gamecontent

import (
	"testing"

	dbsqlite "retrom/internal/database/sqlite"
	application "retrom/internal/service/gamecontent"
)

func TestDeleteImpactIncludesOnlyReleasedArchiveProtection(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "exclusive archive", true: "shared archive"}[shared], func(t *testing.T) {
			db, err := dbsqlite.Open(":memory:", dbsqlite.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if _, err := db.ExecContext(t.Context(), `CREATE TABLE blobs(id TEXT,size_bytes INTEGER,ref_count INTEGER);
   CREATE TABLE archive_entries(archive_blob_id TEXT,materialized_blob_id TEXT);
   INSERT INTO blobs VALUES('member',7,3);
   INSERT INTO archive_entries VALUES('archive','member'),('archive','member'),('archive','archive');`); err != nil {
				t.Fatal(err)
			}
			refs := int64(1)
			if shared {
				refs = 2
			}
			roots := []application.ImpactBlob{
				{ID: "archive", SizeBytes: 13, ProtectiveReferences: refs, GameReferences: 1},
				{ID: "member", SizeBytes: 7, ProtectiveReferences: 3, GameReferences: 1},
			}
			result, err := expandImpactArchives(t.Context(), db, roots)
			if err != nil {
				t.Fatal(err)
			}
			expected := int64(3)
			if shared {
				expected = 1
			}
			if len(result) != 2 || result[1].ID != "member" || result[1].GameReferences != expected {
				t.Fatalf("impact=%+v want member removal count=%d", result, expected)
			}
		})
	}
}
