//go:build integration

package persistence_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func indexedPackage(members int) model.GameFile {
	f := model.GameFile{
		ID: uuid.NewString(), LogicalKey: "game.zip", Role: "project_package", StorageKey: "games/test/package",
		SizeBytes: int64(members)*64 + 1024, SHA256: strings.Repeat("a", 64),
	}
	for i := range members {
		f.Members = append(f.Members, model.GameFileMember{
			LogicalPath: fmt.Sprintf("data/%05d.bin", i),
			DataOffset:  100 + int64(i)*64, SizeBytes: 1, SHA256: strings.Repeat("b", 64), MediaType: "application/octet-stream",
		})
	}
	return f
}

func TestLargePackageIndexPagingAndBoundedRetirement(t *testing.T) {
	f := testsupport.Library(t)
	file := indexedPackage(50_000)
	f.Game.Files = []model.GameFile{file}
	started := time.Now()
	f.Publish(t)
	t.Logf("50000 member insert: %s", time.Since(started))
	physical, err := f.Repository.GameFiles(t.Context(), f.Game.ID)
	if err != nil || len(physical) != 1 {
		t.Fatalf("physical=%d err=%v", len(physical), err)
	}
	page, err := f.Repository.GameContentPage(t.Context(), f.Game.ID, "data/00100.bin", "data/00", 100)
	if err != nil || len(page.Items) != 100 || page.Items[0].LogicalKey != "data/00101.bin" || page.NextCursor == nil {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	member, err := f.Repository.PackageMember(t.Context(), file.ID, "data/49999.bin")
	if err != nil || member.DataOffset != 100+49999*64 {
		t.Fatalf("member=%+v err=%v", member, err)
	}
	started = time.Now()
	logical, err := f.Repository.ContentFiles(t.Context(), physical)
	if err != nil || len(logical) != 50000 {
		t.Fatalf("logical=%d err=%v", len(logical), err)
	}
	t.Logf("50000 member full projection: %s", time.Since(started))
	if err = f.Repository.Execute(t.Context(), "UPDATE game_file_tab SET status='purged',active_slot=NULL,package_slot=NULL WHERE id=$1", file.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.Repository.PurgePackageMembers(t.Context()); err != nil {
		t.Fatal(err)
	}
	remaining, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_member_tab WHERE game_file_id=$1", file.ID)
	if err != nil || remaining != 49000 {
		t.Fatalf("unbounded member cleanup remaining=%d err=%v", remaining, err)
	}
}

func TestDuplicateMemberRollsBackPackageAndGame(t *testing.T) {
	f := testsupport.Library(t)
	file := indexedPackage(2)
	file.Members[1].LogicalPath = file.Members[0].LogicalPath
	f.Game.Files = []model.GameFile{file}
	err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		return tx.CreateGame(t.Context(), f.Principal.User.ID, f.Game, 1000)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("duplicate accepted: %v", err)
	}
	count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_tab WHERE id=$1", f.Game.ID)
	if err != nil || count != 0 {
		t.Fatalf("game escaped rollback count=%d err=%v", count, err)
	}
	count, err = f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_member_tab WHERE game_file_id=$1", file.ID)
	if err != nil || count != 0 {
		t.Fatalf("member escaped rollback count=%d err=%v", count, err)
	}
}

func TestContentMigrationPreservesExistingFiles(t *testing.T) {
	f := testsupport.Library(t)
	f.Publish(t)
	if err := f.Repository.Execute(t.Context(), `DROP TABLE game_file_member_tab;
 DROP INDEX game_file_project_package_idx;
 DELETE FROM schema_migrations_tab WHERE id='003_content_packages'`); err != nil {
		t.Fatal(err)
	}
	if err := f.Repository.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	files, err := f.Repository.GameFiles(t.Context(), f.Game.ID)
	if err != nil || len(files) != 1 || files[0].StorageKey != f.Game.Files[0].StorageKey {
		t.Fatalf("upgrade changed files: %+v err=%v", files, err)
	}
	if _, err = f.Repository.ContentFiles(t.Context(), files); err != nil {
		t.Fatal(err)
	}
}
