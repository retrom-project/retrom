package favorites

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"retrom/internal/capability/content/gametitle"
	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/store"
	"retrom/internal/service/favorites"
	"retrom/internal/testkit/testassert"
	"retrom/internal/testkit/testsupport"
)

const (
	testProfileA = "01980000-0000-7000-8000-00000000a301"
	testProfileB = "01980000-0000-7000-8000-00000000a302"
	testUserA    = "01980000-0000-7000-8000-00000000b301"
	testUserB    = "01980000-0000-7000-8000-00000000b302"
	testGameA    = "01980000-0000-7000-8000-00000000f301"
	testGameB    = "01980000-0000-7000-8000-00000000f302"
	testGameC    = "01980000-0000-7000-8000-00000000f303"
)

func insertFavoriteTestGame(t *testing.T, transaction *sql.Tx, gameID, suffix, title string, year int64) {
	t.Helper()
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO games(
  id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,
  source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),?,?,'','','','',NULL,
 NULLIF(?,0),'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE',?,'[]',?,'PUBLISHED',lower(?),1,1000,1000)
`, gameID, title, gametitle.Initial(title), year, "favorite-test-"+suffix,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", title); err != nil {
		t.Fatal(err)
	}
}

func newFavoriteTestDatabase(t *testing.T) *store.DB {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	if err := testsupport.SeedPlatformInstances(context.Background(), database.SQL); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	if _, err := transaction.ExecContext(context.Background(), "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES
  (?, 'Alice',1000),(?, 'Test',1000);
`, testProfileA, testProfileB); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO users(
  id,profile_id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms
) VALUES
  (?,?,'alice','Alice','USER','ENABLED',1,1,1000,1000),
  (?,?,'test.favorite','Test','USER','ENABLED',1,1,1000,1000)
`, testUserA, testProfileA, testUserB, testProfileB); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES(?,'fixture','ARGON2ID_V1',1000,1000),(?,'fixture','ARGON2ID_V1',1000,1000)
`, testUserA, testUserB); err != nil {
		t.Fatal(err)
	}
	insertFavoriteTestGame(t, transaction, testGameA, "01", "Alpha", 1991)
	insertFavoriteTestGame(t, transaction, testGameB, "02", "Beta", 1992)
	insertFavoriteTestGame(t, transaction, testGameC, "03", "Gamma", 0)
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	return database
}

func decodeResponse[T any](t *testing.T, response favorites.IdempotentResponse) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(response.Body, &value); err != nil {
		t.Fatalf("decode response: %v: %s", err, response.Body)
	}
	return value
}

func favoriteBoundaryID(prefix byte, index int) string {
	return fmt.Sprintf("%c1980000-0000-7000-8000-%012x", prefix, index)
}

func TestServiceFolderLifecycleUndoAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	nowMS := int64(2000)
	service := favorites.New(New(database.SQL), func() time.Time { return time.UnixMilli(nowMS) })
	alice := favorites.Principal{UserID: testUserA, ProfileID: testProfileA}
	test := favorites.Principal{UserID: testUserB, ProfileID: testProfileB}
	keyCreate := "01980000-0000-7000-8000-00000000c301"
	created, err := service.CreateFolder(context.Background(), alice, keyCreate, "  想玩  ", []string{testGameA})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.Status != 201 }), "CreateFolder() = %#v, %v", created, err)
	folder := decodeResponse[favorites.Folder](t, created)
	testassert.Falsef(t, testassert.Any(func() bool { return folder.Name != "想玩" }, func() bool { return folder.VisibleGameCount != 1 }, func() bool { return folder.Version != 1 }), "created folder = %#v", folder)
	replayed, err := service.CreateFolder(context.Background(), alice, keyCreate, "想玩", []string{testGameA})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !replayed.Replayed }, func() bool { return string(replayed.Body) != string(created.Body) }), "create replay = %#v, %v", replayed, err)
	if _, err := service.CreateFolder(context.Background(), alice, keyCreate, "其他", []string{}); !errors.Is(err, favorites.ErrIdempotencyReused) {
		t.Fatalf("reused key error = %v", err)
	}
	if reference, err := service.Reference(context.Background(), testProfileA, testGameA); err != nil ||
		reference == nil || !slices.Equal(reference.FolderIDs, []string{folder.FolderID}) {
		t.Fatalf("Alice reference = %#v, %v", reference, err)
	}
	if reference, err := service.Reference(context.Background(), testProfileB, testGameA); err != nil || reference != nil {
		t.Fatalf("Test reference = %#v, %v", reference, err)
	}
	if _, err := service.ReplaceFolders(context.Background(), test, testGameA, []string{folder.FolderID}); !errors.Is(err, favorites.ErrFolderNotFound) {
		t.Fatalf("cross-owner folder error = %v", err)
	}
	if state, err := service.ReplaceFolders(context.Background(), alice, testGameB, []string{folder.FolderID}); err != nil || !slices.Equal(state.FolderIDs, []string{folder.FolderID}) {
		t.Fatalf("auto favorite = %#v, %v", state, err)
	}
	if state, err := service.ReplaceFolders(context.Background(), alice, testGameB, []string{}); err != nil || len(state.FolderIDs) != 0 {
		t.Fatalf("remove last folder = %#v, %v", state, err)
	}
	unfavoriteKey := "01980000-0000-7000-8000-00000000c302"
	unfavorite, err := service.Unfavorite(context.Background(), alice, unfavoriteKey, []string{testGameA})
	testassert.False(t, err != nil, err)
	snapshot := decodeResponse[favorites.UnfavoriteResult](t, unfavorite)
	testassert.Falsef(t, testassert.Any(func() bool { return len(snapshot.Items) != 1 }, func() bool { return !slices.Equal(snapshot.Items[0].FolderIDs, []string{folder.FolderID}) }), "unfavorite snapshot = %#v", snapshot)
	deleteKey := "01980000-0000-7000-8000-00000000c303"
	deleted, err := service.DeleteFolder(context.Background(), alice, deleteKey, folder.FolderID, folder.Version)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return deleted.Status != 204 }), "DeleteFolder() = %#v, %v", deleted, err)
	restoreKey := "01980000-0000-7000-8000-00000000c304"
	restored, err := service.Restore(context.Background(), alice, restoreKey, []favorites.RestoreItem{
		{GameID: snapshot.Items[0].GameID, FolderIDs: snapshot.Items[0].FolderIDs},
	})
	testassert.False(t, err != nil, err)
	restore := decodeResponse[favorites.RestoreResult](t, restored)
	testassert.Falsef(t, testassert.Any(func() bool { return !slices.Equal(restore.RestoredGameIDs, []string{testGameA}) }, func() bool { return !slices.Equal(restore.SkippedFolderIDs, []string{folder.FolderID}) }), "restore result = %#v", restore)
	if reference, _ := service.Reference(context.Background(), testProfileA, testGameA); reference == nil || len(reference.FolderIDs) != 0 {
		t.Fatalf("restored reference = %#v", reference)
	}
}

func TestServiceConcurrentFavoriteFolderConflictVersionAndLimit(t *testing.T) {
	database := newFavoriteTestDatabase(t)
	service := favorites.New(New(database.SQL), func() time.Time { return time.UnixMilli(2000) })
	alice := favorites.Principal{UserID: testUserA, ProfileID: testProfileA}

	states := make(chan favorites.State, 2)
	errCh := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			state, err := service.Favorite(context.Background(), alice, testGameA)
			states <- state
			errCh <- err
		}()
	}
	wait.Wait()
	close(states)
	close(errCh)
	for err := range errCh {
		testassert.Falsef(t, err != nil, "concurrent favorite: %v", err)
	}
	for state := range states {
		testassert.Falsef(t, state.FavoritedAtMS != 2000, "concurrent favorite state = %#v", state)
	}
	var favoriteRows int
	queryErr := database.SQL.QueryRowContext(context.Background(),
		`SELECT count(*) FROM favorite_games WHERE profile_id=? AND game_id=?`, testProfileA, testGameA,
	).Scan(&favoriteRows)
	testassert.Falsef(t, testassert.Any(func() bool { return queryErr != nil }, func() bool { return favoriteRows != 1 }),
		"concurrent favorite rows = %d, error=%v", favoriteRows, queryErr)

	type createOutcome struct {
		response favorites.IdempotentResponse
		err      error
	}
	outcomes := make(chan createOutcome, 2)
	for index := range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := service.CreateFolder(
				context.Background(), alice, favoriteBoundaryID('6', index+1), "Same Folder", []string{},
			)
			outcomes <- createOutcome{response: response, err: err}
		}()
	}
	wait.Wait()
	close(outcomes)
	created := 0
	conflicts := 0
	var folder favorites.Folder
	for outcome := range outcomes {
		switch {
		case outcome.err == nil:
			created++
			folder = decodeResponse[favorites.Folder](t, outcome.response)
		case errors.Is(outcome.err, favorites.ErrFolderNameConflict):
			conflicts++
		default:
			t.Fatalf("concurrent folder outcome = %#v", outcome)
		}
	}
	testassert.Falsef(t, testassert.Any(func() bool { return created != 1 }, func() bool { return conflicts != 1 }), "concurrent folders = created:%d conflicts:%d", created, conflicts)

	renamed, err := service.RenameFolder(
		context.Background(), alice, favoriteBoundaryID('6', 10), folder.FolderID, "Renamed", folder.Version,
	)
	testassert.False(t, err != nil, err)
	renamedFolder := decodeResponse[favorites.Folder](t, renamed)
	testassert.Falsef(t, testassert.Any(func() bool { return renamedFolder.Version != 2 }, func() bool { return renamed.Headers["ETag"] != `"v2"` }), "renamed folder = %#v headers=%v", renamedFolder, renamed.Headers)
	if _, err := service.RenameFolder(
		context.Background(), alice, favoriteBoundaryID('6', 11), folder.FolderID, "Stale", 1,
	); !errors.Is(err, favorites.ErrVersionConflict) {
		t.Fatalf("stale rename error = %v", err)
	}

	if _, err := database.SQL.ExecContext(context.Background(), `DELETE FROM favorite_folders WHERE profile_id=?`, testProfileA); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	for index := 0; index < favorites.MaxFolders; index++ {
		name := fmt.Sprintf("Folder %03d", index)
		if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,lower(?),1,3000,3000)
`, favoriteBoundaryID('7', index+1), testProfileA, name, name); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateFolder(
		context.Background(), alice, favoriteBoundaryID('6', 12), "Over limit", []string{},
	); !errors.Is(err, favorites.ErrFolderLimit) {
		t.Fatalf("folder limit error = %v", err)
	}
}

func TestOrganizeFaultRollsBackEveryFavoriteMembershipAndIdempotencyRecord(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	service := favorites.New(New(database.SQL), func() time.Time { return time.UnixMilli(2000) })
	alice := favorites.Principal{UserID: testUserA, ProfileID: testProfileA}
	created, err := service.CreateFolder(
		context.Background(), alice, favoriteBoundaryID('6', 20), "Atomic", []string{},
	)
	testassert.False(t, err != nil, err)
	folder := decodeResponse[favorites.Folder](t, created)
	cause := errors.New("injected favorite membership failure")
	fault, assertFault := favoriteMembershipFault(t, database.SQL, cause)
	service = favorites.New(New(fault), func() time.Time { return time.UnixMilli(2000) })
	key := favoriteBoundaryID('6', 21)
	if _, err := service.Organize(
		context.Background(), alice, key,
		[]string{testGameA, testGameB}, []string{folder.FolderID}, []string{},
	); !errors.Is(err, cause) {
		t.Fatalf("Organize() lost injected membership cause: %v", err)
	}
	assertFault()
	var favoriteCount, membershipCount int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM favorite_games WHERE profile_id=?
`, testProfileA).Scan(&favoriteCount); err != nil || favoriteCount != 0 {
		t.Fatalf("favorite rows = %d, error=%v", favoriteCount, err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM favorite_folder_games WHERE profile_id=?
`, testProfileA).Scan(&membershipCount); err != nil || membershipCount != 0 {
		t.Fatalf("membership rows = %d, error=%v", membershipCount, err)
	}
	var failedRecordCount int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT count(*) FROM idempotency_records
WHERE operation_id='postFavoriteOrganize' AND key=?
`, key).Scan(&failedRecordCount); err != nil || failedRecordCount != 0 {
		t.Fatalf("failed organize idempotency rows = %d, error=%v", failedRecordCount, err)
	}
}

func TestServiceAllSortsCursorTuplesSearchAndPlatformSummary(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	nowMS := int64(2000)
	service := favorites.New(New(database.SQL), func() time.Time { return time.UnixMilli(nowMS) })
	alice := favorites.Principal{UserID: testUserA, ProfileID: testProfileA}
	for _, gameID := range []string{testGameA, testGameB, testGameC} {
		if _, err := service.Favorite(context.Background(), alice, gameID); err != nil {
			t.Fatal(err)
		}
		nowMS += 1000
	}
	tests := []struct {
		sort string
		want []string
	}{
		{favorites.SortFavoritedDesc, []string{testGameC, testGameB, testGameA}},
		{favorites.SortRecentlyPlayed, []string{testGameA, testGameB, testGameC}},
		{favorites.SortTitleAsc, []string{testGameA, testGameB, testGameC}},
		{favorites.SortReleaseYearDesc, []string{testGameB, testGameA, testGameC}},
	}
	for _, test := range tests {
		t.Run(test.sort, func(t *testing.T) {
			var cursor *favorites.PageCursor
			got := make([]string, 0, 3)
			for {
				page, err := service.List(context.Background(), alice, favorites.ListOptions{Sort: test.sort, Limit: 1, Cursor: cursor})
				testassert.False(t, err != nil, err)
				testassert.Falsef(t, len(page.Items) != 1, "page items = %#v", page.Items)
				got = append(got, page.Items[0].GameID)
				cursor = page.NextCursor
				if cursor == nil {
					break
				}
			}
			testassert.Truef(t, slices.Equal(got, test.want), "%s order = %v, want %v", test.sort, got, test.want)
		})
	}
	filtered, err := service.List(context.Background(), alice, favorites.ListOptions{Query: "  BETA ", PlatformID: "gba"})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return filtered.TotalCount != 1 }, func() bool { return filtered.Items[0].GameID != testGameB }, func() bool { return len(filtered.Platforms) != 1 }, func() bool { return filtered.Platforms[0].Count != 3 }), "filtered list = %#v, error=%v", filtered, err)
	if _, err := service.List(context.Background(), alice, favorites.ListOptions{
		Sort: favorites.SortTitleAsc, Cursor: &favorites.PageCursor{SortValues: []string{"Alpha"}, ID: "invalid"},
	}); !errors.Is(err, favorites.ErrInvalidCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestServiceListPaginationScopesAndVisibility(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	nowMS := int64(2000)
	service := favorites.New(New(database.SQL), func() time.Time { return time.UnixMilli(nowMS) })
	alice := favorites.Principal{UserID: testUserA, ProfileID: testProfileA}
	if _, err := service.Favorite(context.Background(), alice, testGameA); err != nil {
		t.Fatal(err)
	}
	nowMS = 3000
	if _, err := service.Favorite(context.Background(), alice, testGameB); err != nil {
		t.Fatal(err)
	}
	first, err := service.List(context.Background(), alice, favorites.ListOptions{Sort: favorites.SortFavoritedDesc, Limit: 1})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(first.Items) != 1 }, func() bool { return first.Items[0].GameID != testGameB }, func() bool { return first.NextCursor == nil }, func() bool { return first.Summary.FavoriteCount != 2 }, func() bool { return first.Summary.UncategorizedCount != 2 }), "first page = %#v, %v", first, err)
	second, err := service.List(context.Background(), alice, favorites.ListOptions{
		Sort: favorites.SortFavoritedDesc, Limit: 1, Cursor: first.NextCursor,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(second.Items) != 1 }, func() bool { return second.Items[0].GameID != testGameA }, func() bool { return second.NextCursor != nil }), "second page = %#v, %v", second, err)
	create, err := service.CreateFolder(
		context.Background(), alice, "01980000-0000-7000-8000-00000000c305", "Folder", []string{testGameA},
	)
	testassert.False(t, err != nil, err)
	folder := decodeResponse[favorites.Folder](t, create)
	uncategorized, err := service.List(context.Background(), alice, favorites.ListOptions{Scope: favorites.ScopeUncategorized})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return uncategorized.TotalCount != 1 }, func() bool { return uncategorized.Items[0].GameID != testGameB }), "uncategorized = %#v, %v", uncategorized, err)
	folderPage, err := service.List(context.Background(), alice, favorites.ListOptions{
		Scope: favorites.ScopeFolder, FolderID: folder.FolderID, Sort: favorites.SortTitleAsc,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return folderPage.TotalCount != 1 }, func() bool { return folderPage.Folders[0].VisibleGameCount != 1 }), "folder page = %#v, %v", folderPage, err)
	if _, err := database.SQL.ExecContext(context.Background(), `UPDATE platform_instances SET enabled=0,version=version+1,updated_at_ms=4000 WHERE catalog_template_key='gba/mgba'`); err != nil {
		t.Fatal(err)
	}
	hidden, err := service.List(context.Background(), alice, favorites.ListOptions{})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return hidden.Summary.FavoriteCount != 0 }, func() bool { return hidden.TotalCount != 0 }, func() bool { return hidden.Folders[0].VisibleGameCount != 0 }), "hidden page = %#v, %v", hidden, err)
	var rawCount int
	if err := database.SQL.QueryRowContext(context.Background(), `SELECT count(*) FROM favorite_games WHERE profile_id=?`, testProfileA).Scan(&rawCount); err != nil || rawCount != 2 {
		t.Fatalf("raw favorites = %d, %v", rawCount, err)
	}
}
