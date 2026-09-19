package authn

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestBlocklistNilAndZeroFactsAreEmpty(t *testing.T) {
	t.Parallel()
	for _, blocklist := range []*Blocklist{nil, {}} {
		if blocklist.Contains("password") {
			t.Fatal("empty facts unexpectedly contain a password")
		}
		normalized, err := ValidatePassword("password", "password", "alice", "Alice", blocklist)
		if err != nil || normalized != "password" {
			t.Fatalf("empty facts changed validation: %q, %v", normalized, err)
		}
	}
}

func TestBlocklistNormalizationOwnsFacts(t *testing.T) {
	t.Parallel()
	contents := []byte("CAFE\u0301XY\nStraße!\n" + strings.Repeat("repeated\n", blocklistLines-2))
	blocklist, err := decodeBlocklistLines(contents)
	if err != nil {
		t.Fatal(err)
	}
	for index := range contents {
		contents[index] = 'x'
	}
	for _, query := range []string{"caféxy", "strasse!", "repeated"} {
		if !blocklist.Contains(query) {
			t.Fatalf("lost normalized fact %q after source mutation", query)
		}
	}
	for _, query := range []string{"CAFÉXY", "cafe\u0301xy", "Straße!", " repeated", "repeated "} {
		if blocklist.Contains(query) {
			t.Fatalf("membership normalized or trimmed its query %q", query)
		}
	}
}

func TestBlocklistScannerLineContract(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name     string
		contents string
		valid    bool
	}{
		{"exact line count", strings.Repeat("word\n", blocklistLines), true},
		{"too few lines", strings.Repeat("word\n", blocklistLines-1), false},
		{"too many lines", strings.Repeat("word\n", blocklistLines+1), false},
		{"CRLF lines", strings.Repeat("word\r\n", blocklistLines), true},
		{"final line without newline", strings.TrimSuffix(strings.Repeat("word\n", blocklistLines), "\n"), true},
		{"empty lines still count", strings.Repeat("\n", blocklistLines), true},
		{"largest terminated token", strings.Repeat("x", 4095) + "\n" + strings.Repeat("word\n", blocklistLines-1), true},
		{"scanner token limit", strings.Repeat("x", 4096) + "\n" + strings.Repeat("word\n", blocklistLines-1), false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := decodeBlocklistLines([]byte(testCase.contents))
			if testCase.valid {
				if err != nil || result == nil {
					t.Fatalf("valid lines rejected: %v", err)
				}
				return
			}
			if result != nil || !errors.Is(err, ErrBlocklistInvalid) {
				t.Fatalf("invalid lines accepted: %v", err)
			}
		})
	}
}

func TestDecodeBlocklistRejectsUnpinnedFacts(t *testing.T) {
	t.Parallel()
	for _, contents := range [][]byte{
		nil,
		bytes.Repeat([]byte{'x'}, BlocklistSize-1),
		bytes.Repeat([]byte{'x'}, BlocklistSize),
		bytes.Repeat([]byte{'x'}, BlocklistSize+1),
		bytes.Repeat([]byte{0xff}, BlocklistSize),
	} {
		blocklist, err := DecodeBlocklist(contents)
		if blocklist != nil || !errors.Is(err, ErrBlocklistInvalid) {
			t.Fatalf("unpinned payload accepted: size=%d error=%v", len(contents), err)
		}
	}
}
