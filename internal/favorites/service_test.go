package favorites

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
)

const (
	testProfileA = "01980000-0000-7000-8000-00000000a301"
	testProfileB = "01980000-0000-7000-8000-00000000a302"
	testUserA    = "01980000-0000-7000-8000-00000000b301"
	testUserB    = "01980000-0000-7000-8000-00000000b302"
	testGameA    = "01980000-0000-7000-8000-00000000f301"
	testGameB    = "01980000-0000-7000-8000-00000000f302"
	testGameC    = "01980000-0000-7000-8000-00000000f303"
	instanceGBA  = "01980000-0000-7000-8000-000000000005"
)

func insertFavoriteTestGame(t *testing.T, transaction *sql.Tx, gameID, suffix, title string, year int64) {
	t.Helper()
	metadataID := "01980000-0000-7000-8000-00000000d3" + suffix
	contentID := "01980000-0000-7000-8000-00000000e3" + suffix
	if _, err := transaction.Exec(`
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,'','','','',NULL,NULLIF(?,0),'ADMIN_EDIT',NULL,1000)
`, metadataID, gameID, title, year); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?,'ADMIN_REPLACE','favorite-test','[]',?,1000)
`, contentID, gameID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,'PUBLISHED',?,?,lower(?),1,1000,1000)
`, gameID, instanceGBA, metadataID, contentID, title); err != nil {
		t.Fatal(err)
	}
}

func newFavoriteTestDatabase(t *testing.T) *store.DB {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec("PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO profiles(id,display_name,created_at_ms) VALUES
  (?, 'Alice',1000),(?, 'Test',1000);
`, testProfileA, testProfileB); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO users(
  id,profile_id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms
) VALUES
  (?,?,'alice','Alice','USER','ENABLED',1,1,1000,1000),
  (?,?,'test.favorite','Test','USER','ENABLED',1,1,1000,1000)
`, testUserA, testProfileA, testUserB, testProfileB); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
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

func decodeResponse[T any](t *testing.T, response IdempotentResponse) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(response.Body, &value); err != nil {
		t.Fatalf("decode response: %v: %s", err, response.Body)
	}
	return value
}

func TestNormalizeFolderName(t *testing.T) {
	t.Parallel()
	name, key, err := NormalizeFolderName("  双人\u3000 游戏  ")
	if err != nil || name != "双人 游戏" || key != "双人 游戏" {
		t.Fatalf("normalized = %q/%q, error=%v", name, key, err)
	}
	composed, composedKey, err := NormalizeFolderName("Cafe\u0301")
	if err != nil || composed != "Café" || composedKey != "café" {
		t.Fatalf("NFC/fold = %q/%q, error=%v", composed, composedKey, err)
	}
	for _, invalid := range []string{"", " \t ", "bad\u0000name", string(make([]rune, 41))} {
		if _, _, err := NormalizeFolderName(invalid); !errors.Is(err, ErrInvalidFolderName) {
			t.Fatalf("NormalizeFolderName(%q) error = %v", invalid, err)
		}
	}
	valid40 := "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十"
	if _, _, err := NormalizeFolderName(valid40); err != nil {
		t.Fatalf("40-rune name: %v", err)
	}
	if _, _, err := NormalizeFolderName(strings.Repeat("😀", 40)); err != nil {
		t.Fatalf("160-byte name: %v", err)
	}
}

func favoriteBoundaryID(prefix byte, index int) string {
	return fmt.Sprintf("%c1980000-0000-7000-8000-%012x", prefix, index)
}

func TestBatchAndRestoreNormalizationBoundaries(t *testing.T) {
	t.Parallel()
	games := make([]string, MaxOrganizeGames)
	add := make([]string, MaxOrganizeFolders)
	for index := range games {
		games[index] = favoriteBoundaryID('1', index+1)
	}
	for index := range add {
		add[index] = favoriteBoundaryID('2', index+1)
	}
	canonicalGames, canonicalAdd, canonicalRemove, err := normalizeAndValidateOrganize(games, add, nil)
	if err != nil || len(canonicalGames) != 50 || len(canonicalAdd) != 20 || len(canonicalRemove) != 0 {
		t.Fatalf("maximum organize = %d/%d/%d, error=%v", len(canonicalGames), len(canonicalAdd), len(canonicalRemove), err)
	}
	tooManyGames := append([]string(nil), games...)
	tooManyGames = append(tooManyGames, favoriteBoundaryID('1', 99))
	if _, _, _, err := normalizeAndValidateOrganize(tooManyGames, add, nil); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("51 games error = %v", err)
	}
	if _, _, _, err := normalizeAndValidateOrganize(games[:1], add[:1], add[:1]); !errors.Is(err, ErrInvalid) {
		t.Fatalf("overlapping folders error = %v", err)
	}
	remove := []string{favoriteBoundaryID('3', 1)}
	if _, _, _, err := normalizeAndValidateOrganize(games, add, remove); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("1001 organize edges error = %v", err)
	}
	if _, _, _, err := normalizeAndValidateOrganize([]string{games[0], games[0]}, add[:1], nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate games error = %v", err)
	}

	folders := make([]string, 10)
	for index := range folders {
		folders[index] = favoriteBoundaryID('4', index+1)
	}
	items := make([]RestoreItem, MaxRestoreGames)
	for index := range items {
		items[index] = RestoreItem{GameID: favoriteBoundaryID('5', index+1), FolderIDs: folders}
	}
	canonical, err := normalizeRestoreItems(items)
	if err != nil || len(canonical) != 100 || len(canonical[0].FolderIDs) != 10 {
		t.Fatalf("maximum restore = %#v, error=%v", canonical, err)
	}
	tooManyItems := append([]RestoreItem(nil), items...)
	tooManyItems = append(tooManyItems, RestoreItem{GameID: favoriteBoundaryID('5', 999)})
	if _, err := normalizeRestoreItems(tooManyItems); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("101 restore games error = %v", err)
	}
	overEdges := append([]RestoreItem{}, items...)
	overEdges[0].FolderIDs = append(append([]string{}, folders...), favoriteBoundaryID('4', 99))
	if _, err := normalizeRestoreItems(overEdges); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("1001 restore edges error = %v", err)
	}
	duplicate := append([]RestoreItem{}, items[:2]...)
	duplicate[1].GameID = duplicate[0].GameID
	if _, err := normalizeRestoreItems(duplicate); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate restore game error = %v", err)
	}
}

func TestServiceFolderLifecycleUndoAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	nowMS := int64(2000)
	service := New(database.SQL, func() time.Time { return time.UnixMilli(nowMS) })
	alice := Principal{UserID: testUserA, ProfileID: testProfileA}
	test := Principal{UserID: testUserB, ProfileID: testProfileB}
	keyCreate := "01980000-0000-7000-8000-00000000c301"
	created, err := service.CreateFolder(context.Background(), alice, keyCreate, "  想玩  ", []string{testGameA})
	if err != nil || created.Status != 201 {
		t.Fatalf("CreateFolder() = %#v, %v", created, err)
	}
	folder := decodeResponse[Folder](t, created)
	if folder.Name != "想玩" || folder.VisibleGameCount != 1 || folder.Version != 1 {
		t.Fatalf("created folder = %#v", folder)
	}
	replayed, err := service.CreateFolder(context.Background(), alice, keyCreate, "想玩", []string{testGameA})
	if err != nil || !replayed.Replayed || string(replayed.Body) != string(created.Body) {
		t.Fatalf("create replay = %#v, %v", replayed, err)
	}
	if _, err := service.CreateFolder(context.Background(), alice, keyCreate, "其他", []string{}); !errors.Is(err, ErrIdempotencyReused) {
		t.Fatalf("reused key error = %v", err)
	}
	if reference, err := service.Reference(context.Background(), testProfileA, testGameA); err != nil ||
		reference == nil || !slices.Equal(reference.FolderIDs, []string{folder.FolderID}) {
		t.Fatalf("Alice reference = %#v, %v", reference, err)
	}
	if reference, err := service.Reference(context.Background(), testProfileB, testGameA); err != nil || reference != nil {
		t.Fatalf("Test reference = %#v, %v", reference, err)
	}
	if _, err := service.ReplaceFolders(context.Background(), test, testGameA, []string{folder.FolderID}); !errors.Is(err, ErrFolderNotFound) {
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
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decodeResponse[UnfavoriteResult](t, unfavorite)
	if len(snapshot.Items) != 1 || !slices.Equal(snapshot.Items[0].FolderIDs, []string{folder.FolderID}) {
		t.Fatalf("unfavorite snapshot = %#v", snapshot)
	}
	deleteKey := "01980000-0000-7000-8000-00000000c303"
	deleted, err := service.DeleteFolder(context.Background(), alice, deleteKey, folder.FolderID, folder.Version)
	if err != nil || deleted.Status != 204 {
		t.Fatalf("DeleteFolder() = %#v, %v", deleted, err)
	}
	restoreKey := "01980000-0000-7000-8000-00000000c304"
	restored, err := service.Restore(context.Background(), alice, restoreKey, []RestoreItem{
		{GameID: snapshot.Items[0].GameID, FolderIDs: snapshot.Items[0].FolderIDs},
	})
	if err != nil {
		t.Fatal(err)
	}
	restore := decodeResponse[RestoreResult](t, restored)
	if !slices.Equal(restore.RestoredGameIDs, []string{testGameA}) ||
		!slices.Equal(restore.SkippedFolderIDs, []string{folder.FolderID}) {
		t.Fatalf("restore result = %#v", restore)
	}
	if reference, _ := service.Reference(context.Background(), testProfileA, testGameA); reference == nil || len(reference.FolderIDs) != 0 {
		t.Fatalf("restored reference = %#v", reference)
	}
}

func TestServiceConcurrentFavoriteFolderConflictVersionAndLimit(t *testing.T) {
	database := newFavoriteTestDatabase(t)
	service := New(database.SQL, func() time.Time { return time.UnixMilli(2000) })
	alice := Principal{UserID: testUserA, ProfileID: testProfileA}

	states := make(chan State, 2)
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
		if err != nil {
			t.Fatalf("concurrent favorite: %v", err)
		}
	}
	for state := range states {
		if state.FavoritedAtMS != 2000 {
			t.Fatalf("concurrent favorite state = %#v", state)
		}
	}
	var favoriteRows int
	if err := database.SQL.QueryRow(`SELECT count(*) FROM favorite_games WHERE profile_id=? AND game_id=?`, testProfileA, testGameA).Scan(&favoriteRows); err != nil || favoriteRows != 1 {
		t.Fatalf("concurrent favorite rows = %d, error=%v", favoriteRows, err)
	}

	type createOutcome struct {
		response IdempotentResponse
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
	var folder Folder
	for outcome := range outcomes {
		switch {
		case outcome.err == nil:
			created++
			folder = decodeResponse[Folder](t, outcome.response)
		case errors.Is(outcome.err, ErrFolderNameConflict):
			conflicts++
		default:
			t.Fatalf("concurrent folder outcome = %#v", outcome)
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("concurrent folders = created:%d conflicts:%d", created, conflicts)
	}

	renamed, err := service.RenameFolder(
		context.Background(), alice, favoriteBoundaryID('6', 10), folder.FolderID, "Renamed", folder.Version,
	)
	if err != nil {
		t.Fatal(err)
	}
	renamedFolder := decodeResponse[Folder](t, renamed)
	if renamedFolder.Version != 2 || renamed.Headers["ETag"] != `"v2"` {
		t.Fatalf("renamed folder = %#v headers=%v", renamedFolder, renamed.Headers)
	}
	if _, err := service.RenameFolder(
		context.Background(), alice, favoriteBoundaryID('6', 11), folder.FolderID, "Stale", 1,
	); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale rename error = %v", err)
	}

	if _, err := database.SQL.Exec(`DELETE FROM favorite_folders WHERE profile_id=?`, testProfileA); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxFolders; index++ {
		name := fmt.Sprintf("Folder %03d", index)
		if _, err := transaction.Exec(`
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
	); !errors.Is(err, ErrFolderLimit) {
		t.Fatalf("folder limit error = %v", err)
	}
}

func TestOrganizeFaultRollsBackEveryFavoriteMembershipAndIdempotencyRecord(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	service := New(database.SQL, func() time.Time { return time.UnixMilli(2000) })
	alice := Principal{UserID: testUserA, ProfileID: testProfileA}
	created, err := service.CreateFolder(
		context.Background(), alice, favoriteBoundaryID('6', 20), "Atomic", []string{},
	)
	if err != nil {
		t.Fatal(err)
	}
	folder := decodeResponse[Folder](t, created)
	if _, err := database.SQL.Exec(`
CREATE TRIGGER favorite_test_injected_failure
BEFORE INSERT ON favorite_folder_games
WHEN NEW.game_id='01980000-0000-7000-8000-00000000f302'
BEGIN
  SELECT RAISE(ABORT,'injected favorite membership failure');
END
`); err != nil {
		t.Fatal(err)
	}
	key := favoriteBoundaryID('6', 21)
	if _, err := service.Organize(
		context.Background(), alice, key,
		[]string{testGameA, testGameB}, []string{folder.FolderID}, []string{},
	); err == nil {
		t.Fatal("Organize() succeeded despite injected membership failure")
	}
	var favoriteCount, membershipCount int
	if err := database.SQL.QueryRow(`
SELECT count(*) FROM favorite_games WHERE profile_id=?
`, testProfileA).Scan(&favoriteCount); err != nil || favoriteCount != 0 {
		t.Fatalf("favorite rows = %d, error=%v", favoriteCount, err)
	}
	if err := database.SQL.QueryRow(`
SELECT count(*) FROM favorite_folder_games WHERE profile_id=?
`, testProfileA).Scan(&membershipCount); err != nil || membershipCount != 0 {
		t.Fatalf("membership rows = %d, error=%v", membershipCount, err)
	}
	var failedRecordCount int
	if err := database.SQL.QueryRow(`
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
	service := New(database.SQL, func() time.Time { return time.UnixMilli(nowMS) })
	alice := Principal{UserID: testUserA, ProfileID: testProfileA}
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
		{SortFavoritedDesc, []string{testGameC, testGameB, testGameA}},
		{SortRecentlyPlayed, []string{testGameA, testGameB, testGameC}},
		{SortTitleAsc, []string{testGameA, testGameB, testGameC}},
		{SortReleaseYearDesc, []string{testGameB, testGameA, testGameC}},
	}
	for _, test := range tests {
		t.Run(test.sort, func(t *testing.T) {
			var cursor *PageCursor
			got := make([]string, 0, 3)
			for {
				page, err := service.List(context.Background(), alice, ListOptions{Sort: test.sort, Limit: 1, Cursor: cursor})
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != 1 {
					t.Fatalf("page items = %#v", page.Items)
				}
				got = append(got, page.Items[0].GameID)
				cursor = page.NextCursor
				if cursor == nil {
					break
				}
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("%s order = %v, want %v", test.sort, got, test.want)
			}
		})
	}
	filtered, err := service.List(context.Background(), alice, ListOptions{Query: "  BETA ", PlatformID: "gba"})
	if err != nil || filtered.TotalCount != 1 || filtered.Items[0].GameID != testGameB ||
		len(filtered.Platforms) != 1 || filtered.Platforms[0].Count != 3 {
		t.Fatalf("filtered list = %#v, error=%v", filtered, err)
	}
	if _, err := service.List(context.Background(), alice, ListOptions{
		Sort: SortTitleAsc, Cursor: &PageCursor{SortValues: []string{"Alpha"}, ID: "invalid"},
	}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestServiceListPaginationScopesAndVisibility(t *testing.T) {
	t.Parallel()
	database := newFavoriteTestDatabase(t)
	nowMS := int64(2000)
	service := New(database.SQL, func() time.Time { return time.UnixMilli(nowMS) })
	alice := Principal{UserID: testUserA, ProfileID: testProfileA}
	if _, err := service.Favorite(context.Background(), alice, testGameA); err != nil {
		t.Fatal(err)
	}
	nowMS = 3000
	if _, err := service.Favorite(context.Background(), alice, testGameB); err != nil {
		t.Fatal(err)
	}
	first, err := service.List(context.Background(), alice, ListOptions{Sort: SortFavoritedDesc, Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].GameID != testGameB || first.NextCursor == nil ||
		first.Summary.FavoriteCount != 2 || first.Summary.UncategorizedCount != 2 {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	second, err := service.List(context.Background(), alice, ListOptions{
		Sort: SortFavoritedDesc, Limit: 1, Cursor: first.NextCursor,
	})
	if err != nil || len(second.Items) != 1 || second.Items[0].GameID != testGameA || second.NextCursor != nil {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	create, err := service.CreateFolder(
		context.Background(), alice, "01980000-0000-7000-8000-00000000c305", "Folder", []string{testGameA},
	)
	if err != nil {
		t.Fatal(err)
	}
	folder := decodeResponse[Folder](t, create)
	uncategorized, err := service.List(context.Background(), alice, ListOptions{Scope: ScopeUncategorized})
	if err != nil || uncategorized.TotalCount != 1 || uncategorized.Items[0].GameID != testGameB {
		t.Fatalf("uncategorized = %#v, %v", uncategorized, err)
	}
	folderPage, err := service.List(context.Background(), alice, ListOptions{
		Scope: ScopeFolder, FolderID: folder.FolderID, Sort: SortTitleAsc,
	})
	if err != nil || folderPage.TotalCount != 1 || folderPage.Folders[0].VisibleGameCount != 1 {
		t.Fatalf("folder page = %#v, %v", folderPage, err)
	}
	if _, err := database.SQL.Exec(`UPDATE platform_instances SET enabled=0,version=version+1,updated_at_ms=4000 WHERE id=?`, instanceGBA); err != nil {
		t.Fatal(err)
	}
	hidden, err := service.List(context.Background(), alice, ListOptions{})
	if err != nil || hidden.Summary.FavoriteCount != 0 || hidden.TotalCount != 0 || hidden.Folders[0].VisibleGameCount != 0 {
		t.Fatalf("hidden page = %#v, %v", hidden, err)
	}
	var rawCount int
	if err := database.SQL.QueryRow(`SELECT count(*) FROM favorite_games WHERE profile_id=?`, testProfileA).Scan(&rawCount); err != nil || rawCount != 2 {
		t.Fatalf("raw favorites = %d, %v", rawCount, err)
	}
}
