//go:build integration

package persistence_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/format/titleorder"
	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func publishTitles(t *testing.T, f testsupport.Fixture, titles map[string]string) map[string]string {
	t.Helper()
	ids := make(map[string]string, len(titles))
	for title, sortName := range titles {
		game := f.Game
		game.ID = uuid.NewString()
		game.Input.Title, game.Input.SortName = title, sortName
		game.Files = []model.GameFile{{
			ID: uuid.NewString(), LogicalKey: "game.nes", Role: "ROM",
			StorageKey: "unused-" + game.ID, SizeBytes: 3, SHA256: "content-hash",
		}}
		f.Game = game
		f.Publish(t)
		ids[title] = game.ID
	}
	return ids
}

func listedTitles(t *testing.T, f testsupport.Fixture) string {
	t.Helper()
	page, err := f.Repository.Games(t.Context(), f.Principal.User.ID, "published", model.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, 0, len(page.Items))
	for _, game := range page.Items {
		titles = append(titles, game.TitleLetter+":"+game.Title)
	}
	return strings.Join(titles, " ")
}

func TestListsOrderByLetterAndPinyinWithOtherTitlesLast(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	publishTitles(t, f, map[string]string{
		"坦克大战": "", "Avalon": "", "阿拉丁": "", "1943": "", "超级马里奥1": "",
		"重装机兵": "chong zhuang ji bing", "Zelda": "",
	})
	want := "A:Avalon A:阿拉丁 C:重装机兵 C:超级马里奥1 T:坦克大战 Z:Zelda #:1943"
	if got := listedTitles(t, f); got != want {
		t.Fatalf("order=%s", got)
	}
	letters, err := f.Repository.GameLetters(t.Context(), f.Principal.User.ID, "published", model.Query{})
	if err != nil {
		t.Fatal(err)
	}
	want = "A@0x2 C@2x2 T@4x1 Z@5x1 #@6x1"
	got := make([]string, 0, len(letters))
	for _, letter := range letters {
		got = append(got, letter.Letter+"@"+string(rune('0'+letter.Offset))+"x"+string(rune('0'+letter.Count)))
	}
	if strings.Join(got, " ") != want {
		t.Fatalf("letters=%s", strings.Join(got, " "))
	}
}

func TestStartupRefreshRecomputesOlderRowsOnlyOnce(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	ids := publishTitles(t, f, map[string]string{"魔界村": "", "Battle City": "", "银色": ""})
	// Rows written before this version kept the first character and no key.
	if err := f.Repository.Execute(t.Context(), `UPDATE game_tab SET title_initial=left(title,1),title_sort_key='',
 title_order_version=0`); err != nil {
		t.Fatal(err)
	}
	refreshed, err := f.Repository.RefreshTitleOrder(t.Context())
	if err != nil || refreshed != 3 {
		t.Fatalf("refreshed=%d err=%v", refreshed, err)
	}
	if got := listedTitles(t, f); got != "B:Battle City M:魔界村 Y:银色" {
		t.Fatalf("order=%s", got)
	}
	refreshed, err = f.Repository.RefreshTitleOrder(t.Context())
	if err != nil || refreshed != 0 {
		t.Fatalf("current rows were recomputed again: %d %v", refreshed, err)
	}
	count, err := f.Repository.Count(t.Context(), `SELECT count(*) FROM game_tab WHERE id=$1 AND title_order_version=$2`,
		ids["银色"], titleorder.Version)
	if err != nil || count != 1 {
		t.Fatalf("version not stored: %d %v", count, err)
	}
}
