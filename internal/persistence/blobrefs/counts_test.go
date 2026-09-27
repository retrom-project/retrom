package blobrefs

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/database/sqlite"
)

func countDatabase(t *testing.T) dbapi.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "counts.db"), sqlite.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = db.ExecContext(t.Context(), `CREATE TABLE blobs(id TEXT PRIMARY KEY,ref_count INTEGER NOT NULL);
CREATE TABLE archive_entries(archive_blob_id TEXT,materialized_blob_id TEXT);
INSERT INTO blobs VALUES('archive',0),('member',0),('shared',0);
INSERT INTO archive_entries VALUES('archive','member'),('archive','member'),('archive','archive');`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func applyCount(t *testing.T, db dbapi.DB, delta Delta) []string {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	zero, err := Apply(t.Context(), tx, delta)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return zero.Zero
}

func assertCount(t *testing.T, db dbapi.Executor, id string, want int64) {
	t.Helper()
	var actual int64
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT ref_count FROM blobs WHERE id=?`, id).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != want {
		t.Fatalf("%s count=%d, want %d", id, actual, want)
	}
}

func TestCountsProtectSharedArchiveMembersAndReturnIndirectZeros(t *testing.T) {
	db := countDatabase(t)
	applyCount(t, db, Delta{"archive": 2, "member": 1})
	assertCount(t, db, "archive", 2)
	assertCount(t, db, "member", 3)
	applyCount(t, db, Delta{"archive": -1})
	assertCount(t, db, "member", 3)
	zero := applyCount(t, db, Delta{"archive": -1})
	if !reflect.DeepEqual(zero, []string{"archive"}) {
		t.Fatalf("zero=%v", zero)
	}
	assertCount(t, db, "member", 1)
	applyCount(t, db, Delta{"archive": 1})
	zero = applyCount(t, db, Delta{"archive": -1, "member": -1})
	if !reflect.DeepEqual(zero, []string{"archive", "member"}) {
		t.Fatalf("zero=%v", zero)
	}
}

func TestInvalidCountsRollBackAllEarlierChanges(t *testing.T) {
	db := countDatabase(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(t.Context(), tx, Delta{"archive": 1, "shared": -1})
	if !errors.Is(err, ErrCount) {
		t.Fatalf("error=%v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "archive", 0)
	assertCount(t, db, "member", 0)
}

func TestCountsRejectOverflowAndMissingBlobs(t *testing.T) {
	db := countDatabase(t)
	applyCount(t, db, Delta{"shared": math.MaxInt64})
	for _, delta := range []Delta{{"shared": 1}, {"absent": 1}} {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Apply(t.Context(), tx, delta); err == nil {
			t.Error("accepted invalid reference")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, db, "shared", math.MaxInt64)
}

func TestCountsRespectCancellation(t *testing.T) {
	db := countDatabase(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Apply(ctx, db, Delta{"archive": 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
