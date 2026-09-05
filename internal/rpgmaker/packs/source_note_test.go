package packs

import (
	"errors"
	"testing"
)

func TestSourceNoteNormalizationAndBounds(t *testing.T) {
	value, err := normalizeSourceNote("  Cafe\u0301\rtest  ")
	if err != nil || value != "Café\ntest" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	for _, invalid := range []string{"bad\x00note", string(make([]byte, 2001))} {
		if _, err := normalizeSourceNote(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid note error=%v", err)
		}
	}
}
