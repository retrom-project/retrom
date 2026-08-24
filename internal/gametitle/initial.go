package gametitle

import (
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

const fallbackInitial = "#"

var firstLetterArgs = func() pinyin.Args {
	arguments := pinyin.NewArgs()
	arguments.Style = pinyin.FirstLetter
	return arguments
}()

// Initial returns the persisted navigation group for the first rune of a game title.
func Initial(title string) string {
	character := firstRune(title)
	switch {
	case character >= '0' && character <= '9':
		return string(character)
	case character >= 'A' && character <= 'Z':
		return string(character)
	case character >= 'a' && character <= 'z':
		return string(character - 'a' + 'A')
	case unicode.Is(unicode.Han, character):
		return hanInitial(character)
	default:
		return fallbackInitial
	}
}

func firstRune(value string) rune {
	for _, character := range value {
		return character
	}
	return 0
}

func hanInitial(character rune) string {
	readings := pinyin.SinglePinyin(character, firstLetterArgs)
	if len(readings) == 0 || len(readings[0]) != 1 {
		return fallbackInitial
	}
	initial := readings[0][0]
	if initial >= 'a' && initial <= 'z' {
		return string(initial - 'a' + 'A')
	}
	if initial >= 'A' && initial <= 'Z' {
		return string(initial)
	}
	return fallbackInitial
}
