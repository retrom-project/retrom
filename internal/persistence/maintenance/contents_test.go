package maintenance

import (
	"testing"

	dbsqlite "retrom/internal/database/sqlite"
)

func TestBackupIncludesEveryRegisteredBlobEvenWithoutOwners(t *testing.T) {
	db, err := dbsqlite.Open(":memory:", dbsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE blobs(sha256 TEXT,size_bytes INTEGER,ref_count INTEGER);
 INSERT INTO blobs VALUES('protected',17,1),('unreferenced',23,0);`); err != nil {
		t.Fatal(err)
	}
	blobs, err := backupBlobs(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 2 || blobs[0].SHA256 != "protected" || blobs[0].SizeBytes != 17 || blobs[1].SHA256 != "unreferenced" || blobs[1].SizeBytes != 23 {
		t.Fatalf("backup omitted registered data: %+v", blobs)
	}
}
