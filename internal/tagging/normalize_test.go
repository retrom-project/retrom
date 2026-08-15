package tagging

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, display, key, search string
	}{
		{"spaces", "  双人\u3000 游戏 ", "双人 游戏", "双人 游戏", "双人 游戏"},
		{"NFC", "Cafe\u0301", "Café", "café", "café"},
		{"case fold", "ACTION", "ACTION", "action", "action"},
		{"emoji", "🎮  合作", "🎮 合作", "🎮 合作", "🎮 合作"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			display, key, search, err := NormalizeName(test.input)
			if err != nil || display != test.display || key != test.key || search != test.search {
				t.Fatalf("NormalizeName(%q) = %q/%q/%q, %v", test.input, display, key, search, err)
			}
		})
	}
}

func TestNormalizeNameBoundaries(t *testing.T) {
	t.Parallel()
	forty := strings.Repeat("界", 40)
	if value, _, _, err := NormalizeName(forty); err != nil || utf8.RuneCountInString(value) != 40 {
		t.Fatalf("40 code points = %q, %v", value, err)
	}
	for _, value := range []string{"", "  ", "bad\nname", strings.Repeat("界", 41)} {
		if _, _, _, err := NormalizeName(value); !errors.Is(err, ErrNameInvalid) {
			t.Fatalf("NormalizeName(%q) error = %v", value, err)
		}
	}
}

func TestValidateIDsRejectsDuplicatesAndLimit(t *testing.T) {
	t.Parallel()
	id := "01980000-0000-7000-8000-000000000001"
	if _, err := ValidateIDs([]string{id, id}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate error = %v", err)
	}
	values := make([]string, MaxTagsPerOwner+1)
	for index := range values {
		values[index] = id
	}
	if _, err := ValidateIDs(values); !errors.Is(err, ErrAssignmentLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
	if _, err := ValidateIDs([]string{"550e8400-e29b-41d4-a716-446655440000"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-v7 error = %v", err)
	}
}
