package favorites

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func favoriteBoundaryID(prefix byte, index int) string {
	return fmt.Sprintf("%c1980000-0000-7000-8000-%012x", prefix, index)
}

func TestNormalizeFolderName(t *testing.T) {
	t.Parallel()
	name, key, err := NormalizeFolderName("  双人\u3000 游戏  ")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return name != "双人 游戏" }, func() bool { return key != "双人 游戏" }), "normalized = %q/%q, error=%v", name, key, err)
	composed, composedKey, err := NormalizeFolderName("Cafe\u0301")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return composed != "Café" }, func() bool { return composedKey != "café" }), "NFC/fold = %q/%q, error=%v", composed, composedKey, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(canonicalGames) != 50 }, func() bool { return len(canonicalAdd) != 20 }, func() bool { return len(canonicalRemove) != 0 }), "maximum organize = %d/%d/%d, error=%v", len(canonicalGames), len(canonicalAdd), len(canonicalRemove), err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(canonical) != 100 }, func() bool { return len(canonical[0].FolderIDs) != 10 }), "maximum restore = %#v, error=%v", canonical, err)
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
