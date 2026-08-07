package httpapi

import (
	"strings"
	"testing"
)

func TestPlatformSlugBaseUsesReadableASCIIOrPlatformFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		platformID string
		want       string
	}{
		{name: "My GBA Games", platformID: "gba", want: "my-gba-games"},
		{name: "街机格斗游戏", platformID: "arcade", want: "arcade-library"},
		{name: "GBA 游戏", platformID: "gba", want: "gba"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := platformSlugBase(test.name, test.platformID); got != test.want {
				t.Fatalf("platformSlugBase(%q, %q) = %q, want %q", test.name, test.platformID, got, test.want)
			}
		})
	}
}

func TestPlatformSlugSuffixStaysWithinStorageLimit(t *testing.T) {
	t.Parallel()
	base := strings.Repeat("a", 80)
	if got := platformSlugWithSuffix(base, 12); len(got) != 80 || !strings.HasSuffix(got, "-12") {
		t.Fatalf("suffixed slug = %q (%d bytes)", got, len(got))
	}
}
