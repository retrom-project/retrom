package storageanalysis

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	application "retrom/internal/service/storageanalysis"
	"retrom/internal/store"
)

func TestAnalyzeCountsSameDigestSeparatelyAndClassifiesByOwner(t *testing.T) {
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	for _, file := range []struct {
		id, kind, owner string
		size            int64
		retired         any
	}{
		{"game-a", "GAME", "a", 100, nil},
		{"game-b", "GAME", "b", 100, nil},
		{"bios", "BIOS_INSTALLATION", "bios", 200, nil},
		{"save", "SAVE_STATE", "save", 300, nil},
		{"review", "IMPORT_ITEM", "item", 400, nil},
		{"old", "GAME", "a", 500, 10},
	} {
		_, err := database.SQL.ExecContext(t.Context(), `INSERT INTO stored_files(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms,owner_kind,owner_id,retired_at_ms)
VALUES(?,printf('%064d',1),?,printf('%032d',1),printf('%040d',1),'00000001','application/octet-stream',0,?,?,?)`, file.id, file.size, file.kind, file.owner, file.retired)
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := application.New(New(database.ReadOnly), func() time.Time { return time.UnixMilli(1234) }).Analyze(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := application.Totals{RegisteredBytes: 1600, RetainedBytes: 1100, PendingDeleteBytes: 500, FileCount: 6}
	if snapshot.Totals != want || snapshot.GeneratedAtMS != 1234 || len(snapshot.Categories) != 6 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.Categories[0].Bytes != 200 || snapshot.Categories[0].FileCount != 2 {
		t.Fatalf("same bytes merged: %+v", snapshot.Categories)
	}
	if _, err := database.SQL.ExecContext(t.Context(), `UPDATE stored_files SET owner_kind='UNKNOWN' WHERE id='review'`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.New(New(database.ReadOnly), time.Now).Analyze(t.Context()); err == nil {
		t.Fatal("unknown owner silently classified")
	}
}

func TestAnalyzeSurfacesReadDatabaseFailure(t *testing.T) {
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReadOnly.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.SQL.Close()) })
	if _, err := application.New(New(database.ReadOnly), time.Now).Analyze(context.Background()); err == nil {
		t.Fatal("Analyze succeeded with closed read-only database")
	}
}
