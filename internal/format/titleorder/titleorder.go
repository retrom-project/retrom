// Package titleorder places game titles in the letter groups A-Z and # and gives them a sort key
// that orders Chinese by pinyin, so Latin and Chinese titles of one letter sit together.
package titleorder

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Version changes whenever Of returns different results; stored rows of older versions are recomputed.
const Version = 1

// OtherGroup holds digits, symbols, kana and anything without a Latin or pinyin initial.
const OtherGroup = "#"

// Long titles are ordered by their first runes, keeping index keys small; title and id break ties.
const keyRunes = 48

// First characters of each pinyin initial under CLDR zh collation. No syllable starts with I, U or V.
var pinyinGroups = []struct{ letter, first string }{
	{"A", "阿"},
	{"B", "八"},
	{"C", "嚓"},
	{"D", "哒"},
	{"E", "妸"},
	{"F", "发"},
	{"G", "旮"},
	{"H", "哈"},
	{"J", "讥"},
	{"K", "咔"},
	{"L", "垃"},
	{"M", "妈"},
	{"N", "拏"},
	{"O", "噢"},
	{"P", "妑"},
	{"Q", "七"},
	{"R", "呥"},
	{"S", "仨"},
	{"T", "他"},
	{"W", "屲"},
	{"X", "夕"},
	{"Y", "丫"},
	{"Z", "帀"},
}

var (
	mutex    sync.Mutex
	collator = collate.New(language.Chinese)
	// Groups compare base letters only: case, accents and full-width forms do not move a title.
	grouping = collate.New(language.Chinese, collate.Loose)
	buffer   collate.Buffer
)

// Order is the stored letter group and sort key of a title.
type Order struct {
	Letter string
	Key    string
}

// Of orders a game by its sort name when present, otherwise by its title.
func Of(title, sortName string) Order {
	source := strings.TrimSpace(sortName)
	if source == "" {
		source = strings.TrimSpace(title)
	}
	runes := []rune(source)
	if len(runes) > keyRunes {
		runes = runes[:keyRunes]
	}
	mutex.Lock()
	defer mutex.Unlock()
	letter := OtherGroup
	if len(runes) > 0 {
		letter = group(runes[0])
	}
	key := collator.KeyFromString(&buffer, string(runes))
	order := Order{Letter: letter, Key: fmt.Sprintf("%02d", rank(letter)) + hex.EncodeToString(key)}
	buffer.Reset()
	return order
}

func group(first rune) string {
	value := string(first)
	switch {
	case unicode.Is(unicode.Latin, first):
		letter := OtherGroup
		for candidate := 'A'; candidate <= 'Z'; candidate++ {
			if grouping.CompareString(value, string(candidate)) >= 0 {
				letter = string(candidate)
			}
		}
		return letter
	case unicode.Is(unicode.Han, first):
		letter := OtherGroup
		for _, candidate := range pinyinGroups {
			if grouping.CompareString(value, candidate.first) >= 0 {
				letter = candidate.letter
			}
		}
		return letter
	default:
		return OtherGroup
	}
}

// rank places A-Z in order and the other group last.
func rank(letter string) int {
	if letter == OtherGroup {
		return 26
	}
	return int(letter[0] - 'A')
}
