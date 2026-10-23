package titleorder

import (
	"sort"
	"strings"
	"testing"
)

func TestTitlesGroupByLatinLetterOrPinyinInitial(t *testing.T) {
	t.Parallel()
	for title, letter := range map[string]string{
		"Avalon": "A", "阿拉丁": "A", "超级马里奥1": "C", "绯色修仙录": "F", "洛克人": "L", "魔界村": "M",
		"水色": "S", "坦克大战": "T", "星之梦": "X", "银色": "Y", "Zelda": "Z", "zelda": "Z",
		"Ｍｅｇａ": "M", "Écran": "E", "007 决斗": "#", "1943": "#", "どきどき": "#", "★FX单元小雪": "#",
		"  魂斗罗": "H", "": "#",
	} {
		if got := Of(title, "").Letter; got != letter {
			t.Errorf("%q grouped as %q, want %q", title, got, letter)
		}
	}
}

func TestSortNameOverridesPolyphonicTitles(t *testing.T) {
	t.Parallel()
	if Of("重装机兵", "").Letter != "Z" {
		t.Fatal("the default reading of 重 changed; review the override example")
	}
	order := Of("重装机兵", "chong zhuang ji bing")
	if order.Letter != "C" || order.Key != Of("其他标题", " chong zhuang ji bing ").Key {
		t.Fatalf("sort name was not used: %+v", order)
	}
}

func TestKeysOrderLettersTogetherAndOtherTitlesLast(t *testing.T) {
	t.Parallel()
	titles := []string{"1943", "坦克大战", "Zelda", "阿拉丁", "Avalon", "超级马里奥1", "超级马里奥2", "どきどき", "Battle City"}
	sort.Slice(titles, func(i, j int) bool { return Of(titles[i], "").Key < Of(titles[j], "").Key })
	want := "Avalon 阿拉丁 Battle City 超级马里奥1 超级马里奥2 坦克大战 Zelda 1943 どきどき"
	if got := strings.Join(titles, " "); got != want {
		t.Fatalf("order=%s", got)
	}
}

func TestLongTitlesKeepBoundedDeterministicKeys(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("超级马里奥兄弟", 40)
	first, second := Of(long, ""), Of(long+"续", "")
	if first.Key != second.Key || len(first.Key) > 2000 {
		t.Fatalf("long title key not bounded: %d", len(first.Key))
	}
}
