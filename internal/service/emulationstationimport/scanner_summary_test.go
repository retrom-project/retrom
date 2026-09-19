package emulationstationimport

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	model "retrom/internal/model/emulationstationimport"
	"retrom/internal/testkit/testassert"
)

func TestReferencedDiscPathsExcludeUnrelatedCHDs(t *testing.T) {
	t.Parallel()
	exact := map[string]string{
		"one.chd":    "disc/one.chd",
		"TWO.CHD":    "disc/TWO.CHD",
		"unused.chd": "disc/unused.chd",
	}
	folded := map[string][]string{
		"one.chd":    {"disc/one.chd"},
		"two.chd":    {"disc/TWO.CHD"},
		"unused.chd": {"disc/unused.chd"},
	}
	paths := referencedDiscPaths([]string{"one.chd", "two.chd", "missing.chd"}, exact, folded)
	sort.Strings(paths)
	if got, want := strings.Join(paths, "|"), "disc/TWO.CHD|disc/one.chd"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestSnapshotDigestUsesTheSpecifiedCanonicalFieldOrder(t *testing.T) {
	t.Parallel()
	gamelists := []model.ScanGamelist{{
		Path: "nes/gamelist.xml", Size: 42, Digest: "content",
		Facts: "facts", State: "VALID",
	}}
	canonical := []byte(
		`{"schemaVersion":1,"gamelists":[{"path":"nes/gamelist.xml","sizeBytes":42,` +
			`"contentDigest":"content","factsDigest":"facts","parseState":"VALID"}]}`,
	)
	want := sha256.Sum256(canonical)
	testassert.Falsef(t, snapshotDigest(gamelists) != hex.EncodeToString(want[:]),
		"snapshot digest did not use the documented canonical JSON")
}
