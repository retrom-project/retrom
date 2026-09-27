package emulationstationmeta

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDeclaredPathAcceptsESRelativeForms(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"game.gba":                   "game.gba",
		"./game.gba":                 "game.gba",
		"././nested/game.gba":        "nested/game.gba",
		`.\nested\game.gba`:          "nested/game.gba",
		"nested/space name.gba":      "nested/space name.gba",
		"日文/ゲーム.gba":                 "日文/ゲーム.gba",
		"nested/internal\u2003x.gba": "nested/internal\u2003x.gba",
	}
	for declared, expected := range tests {
		t.Run(declared, func(t *testing.T) {
			t.Parallel()
			actual, err := NormalizeDeclaredPath(declared)
			if err != nil || actual != expected {
				t.Fatalf("NormalizeDeclaredPath(%q) = %q, %v; want %q", declared, actual, err, expected)
			}
		})
	}
}

func TestNormalizeDeclaredPathRejectsUnsafeOrNonCanonicalForms(t *testing.T) {
	t.Parallel()
	tooLongSegment := strings.Repeat("a", MaxDeclaredSegmentBytes+1)
	tooLongPath := strings.Repeat("a/", MaxDeclaredPathBytes/2) + "a"
	tests := []string{
		"", " ", " game.gba", "game.gba ", "\u00a0game.gba", "game.gba\u00a0", "\x00game.gba",
		"/absolute/game.gba", `\absolute\game.gba`, `\\server\share\game.gba`, "~/game.gba",
		`C:\game.gba`, `c:/game.gba`, "https://example.invalid/game.gba", "file:game.gba",
		"../game.gba", "nested/../game.gba", "nested/./game.gba", "nested//game.gba", "nested/",
		"nested\n/game.gba", tooLongSegment + "/game.gba", tooLongPath,
	}
	for _, declared := range tests {
		t.Run(strings.ReplaceAll(declared, "/", "_"), func(t *testing.T) {
			t.Parallel()
			actual, err := NormalizeDeclaredPath(declared)
			if !errors.Is(err, ErrPathInvalid) || actual != "" {
				t.Fatalf("NormalizeDeclaredPath(%q) = %q, %v", declared, actual, err)
			}
		})
	}
}

func TestNormalizeDeclaredPathAppliesByteRatherThanRuneLimits(t *testing.T) {
	t.Parallel()
	validSegment := strings.Repeat("界", MaxDeclaredSegmentBytes/3)
	if _, err := NormalizeDeclaredPath(validSegment + "/game.gba"); err != nil {
		t.Fatalf("valid multibyte segment rejected: %v", err)
	}
	invalidSegment := validSegment + "界"
	if _, err := NormalizeDeclaredPath(invalidSegment + "/game.gba"); !errors.Is(err, ErrPathInvalid) {
		t.Fatalf("overlong multibyte segment error = %v", err)
	}
}
