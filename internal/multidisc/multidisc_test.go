package multidisc

import (
	"bytes"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestParseAcceptsBoundedPlaylistAndBuildsCanonicalView(t *testing.T) {
	t.Parallel()
	playlist := append([]byte{0xef, 0xbb, 0xbf}, []byte("# comment\r\nDisc One.CHD\r\n光盘二.chd\r\n")...)
	result, err := Parse(playlist, []File{
		{Basename: "disc one.chd", LogicalName: "games/disc one.chd", SizeBytes: 8, Header: []byte("MComprHD")},
		{Basename: "光盘二.chd", LogicalName: "games/光盘二.chd", SizeBytes: 9, Header: []byte("MComprHD")},
	}, DefaultLimits())
	testassert.False(t, err != nil, err)
	if got, want := string(result.CanonicalPlaylist), "disc-001.chd\ndisc-002.chd\n"; got != want {
		t.Fatalf("canonical playlist = %q, want %q", got, want)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return result.PresentTotalBytes != 17 }, func() bool { return result.Entries[0].State != EntryPresent }, func() bool { return result.Entries[0].File.Basename != "disc one.chd" }, func() bool { return result.Entries[1].NormalizedReference != "光盘二.chd" }), "result = %#v", result)
}

func TestParsePreservesUnicodeBytesWithoutNormalization(t *testing.T) {
	t.Parallel()
	nfc := "é.chd"
	nfd := "e\u0301.chd"
	result, err := Parse([]byte(nfc+"\n"+nfd+"\n"), []File{
		{Basename: nfc, SizeBytes: 8, Header: []byte("MComprHD")},
	}, DefaultLimits())
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Entries[0].State != EntryPresent }, func() bool { return result.Entries[1].State != EntryMissing }, func() bool { return result.Entries[1].NormalizedReference != nfd }), "Unicode matching was normalized: %#v", result.Entries)
}

func TestParseBoundaries(t *testing.T) {
	t.Parallel()
	eight := make([]string, MaxDiscs)
	files := make([]File, MaxDiscs)
	for index := range eight {
		eight[index] = "disc" + string(rune('a'+index)) + ".chd"
		files[index] = File{Basename: eight[index], SizeBytes: 1, Header: []byte("MComprHD")}
	}
	if _, err := Parse([]byte(strings.Join(eight, "\n")+"\n"), files, DefaultLimits()); err != nil {
		t.Fatalf("eight discs: %v", err)
	}
	if _, err := Parse([]byte("one.chd\n"), nil, DefaultLimits()); !ErrorHasCode(err, CodePlaylistInvalid) {
		t.Fatalf("one disc error = %v", err)
	}
	nine := append(append([]string{}, eight...), "ninth.chd")
	if _, err := Parse([]byte(strings.Join(nine, "\n")+"\n"), nil, DefaultLimits()); !ErrorHasCode(err, CodeLimitExceeded) {
		t.Fatalf("nine disc error = %v", err)
	}
	exactLimit := append([]byte("one.chd\ntwo.chd\n#"), bytes.Repeat([]byte{'x'}, MaxPlaylistBytes-17)...)
	testassert.Falsef(t, len(exactLimit) != MaxPlaylistBytes, "test playlist size = %d", len(exactLimit))
	if _, err := Parse(exactLimit, nil, DefaultLimits()); err != nil {
		t.Fatalf("65,536 bytes: %v", err)
	}
	if _, err := Parse(append(exactLimit, 'x'), nil, DefaultLimits()); !ErrorHasCode(err, CodeLimitExceeded) {
		t.Fatalf("65,537 bytes error = %v", err)
	}
	limits := DefaultLimits()
	limits.MaxTotalBytes = 16
	if _, err := Parse([]byte("a.chd\nb.chd\n"), []File{
		{Basename: "a.chd", SizeBytes: 8, Header: []byte("MComprHD")},
		{Basename: "b.chd", SizeBytes: 9, Header: []byte("MComprHD")},
	}, limits); !ErrorHasCode(err, CodeLimitExceeded) {
		t.Fatalf("total limit error = %v", err)
	}
}

func TestReferencesReturnsOnlyValidatedPlaylistEntries(t *testing.T) {
	t.Parallel()
	references, err := References(
		[]byte("# ignored\r\nDisc One.CHD\r\ntwo.chd\r\n"),
		DefaultLimits(),
	)
	testassert.False(t, err != nil, err)
	if got, want := strings.Join(references, "|"), "Disc One.CHD|two.chd"; got != want {
		t.Fatalf("references = %q, want %q", got, want)
	}
	if _, err := References([]byte("../one.chd\ntwo.chd\n"), DefaultLimits()); !ErrorHasCode(err, CodeReferenceUnsafe) {
		t.Fatalf("unsafe reference error = %v", err)
	}
}

func TestParseRejectsUnsafeOrInvalidReferences(t *testing.T) {
	t.Parallel()
	unsafe := []string{
		"", "one.chd", "../one.chd\ntwo.chd", "dir/one.chd\ntwo.chd", "dir\\one.chd\ntwo.chd",
		"https:one.chd\ntwo.chd", "one.chd?x\ntwo.chd", "one.chd#x\ntwo.chd", ".\ntwo.chd",
		" one.chd\ntwo.chd", "one.chd \ntwo.chd", "one\x00.chd\ntwo.chd", "one\r.chd\ntwo.chd",
	}
	for _, playlist := range unsafe {
		t.Run(strings.ReplaceAll(playlist, "\n", "_"), func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(playlist), nil, DefaultLimits())
			testassert.False(t, err == nil, "Parse() accepted unsafe playlist")
		})
	}
	invalid := []string{
		"one.cue\ntwo.chd\n", "one.chd\nONE.CHD\n", string([]byte{0xff, '\n'}) + "two.chd\n",
	}
	for _, playlist := range invalid {
		if _, err := Parse([]byte(playlist), nil, DefaultLimits()); !ErrorHasCode(err, CodePlaylistInvalid) {
			t.Fatalf("playlist %q error = %v", playlist, err)
		}
	}
}

func TestParseMatchesExactlyThenUniqueASCIIFoldAndAllowsMissing(t *testing.T) {
	t.Parallel()
	result, err := Parse([]byte("Exact.chd\nSECOND.CHD\nmissing.chd\n"), []File{
		{Basename: "Exact.chd", SizeBytes: 8, Header: []byte("MComprHD")},
		{Basename: "second.chd", SizeBytes: 8, Header: []byte("MComprHD")},
	}, DefaultLimits())
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Entries[0].File.Basename != "Exact.chd" }, func() bool { return result.Entries[1].File.Basename != "second.chd" }, func() bool { return result.Entries[2].State != EntryMissing }, func() bool { return result.Entries[2].File != nil }), "entries = %#v", result.Entries)
}

func TestParseExactMatchWinsOverASCIIFoldCollision(t *testing.T) {
	t.Parallel()
	result, err := Parse([]byte("Exact.chd\ntwo.chd\n"), []File{
		{Basename: "Exact.chd", SizeBytes: 8, Header: []byte("MComprHD")},
		{Basename: "exact.chd", SizeBytes: 8, Header: []byte("MComprHD")},
	}, DefaultLimits())
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Entries[0].State != EntryPresent }, func() bool { return result.Entries[0].File.Basename != "Exact.chd" }), "exact entry = %#v", result.Entries[0])
	if _, err := Parse([]byte("EXACT.CHD\ntwo.chd\n"), []File{
		{Basename: "Exact.chd"}, {Basename: "exact.chd"},
	}, DefaultLimits()); !ErrorHasCode(err, CodePlaylistInvalid) {
		t.Fatalf("ambiguous fallback error = %v", err)
	}
}

func TestParseRejectsInvalidCHDAndDirectoryConflicts(t *testing.T) {
	t.Parallel()
	for name, file := range map[string]File{
		"empty":     {Basename: "one.chd", SizeBytes: 0, Header: nil},
		"short":     {Basename: "one.chd", SizeBytes: 8, Header: []byte("short")},
		"bad magic": {Basename: "one.chd", SizeBytes: 8, Header: []byte("NotCHD!!")},
	} {
		if _, err := Parse([]byte("one.chd\ntwo.chd\n"), []File{file}, DefaultLimits()); !ErrorHasCode(err, CodeCHDInvalid) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if _, err := Parse([]byte("OnE.chd\ntwo.chd\n"), []File{
		{Basename: "ONE.chd"}, {Basename: "one.chd"},
	}, DefaultLimits()); !ErrorHasCode(err, CodePlaylistInvalid) {
		t.Fatalf("ambiguous directory error = %v", err)
	}
}

func TestIdentityExpectedSetGroupAndAttachmentState(t *testing.T) {
	t.Parallel()
	discA := strings.Repeat("a", 64)
	discB := strings.Repeat("b", 64)
	base, err := ContentIdentity([]string{discA, discB})
	testassert.False(t, err != nil, err)
	same, _ := ContentIdentity([]string{discA, discB})
	reordered, _ := ContentIdentity([]string{discB, discA})
	changed, _ := ContentIdentity([]string{discA, strings.Repeat("c", 64)})
	testassert.Falsef(t, testassert.Any(func() bool { return base != same }, func() bool { return base == reordered }, func() bool { return base == changed }), "identities = %q %q %q %q", base, same, reordered, changed)
	entries := []Entry{
		{Ordinal: 0, SourceReference: "ONE.CHD", NormalizedReference: "one.chd", State: EntryMissing},
		{Ordinal: 1, SourceReference: "two.chd", NormalizedReference: "two.chd", State: EntryPresent},
		{Ordinal: 2, SourceReference: "three.chd", NormalizedReference: "three.chd", State: EntryMissing},
	}
	digest, err := ExpectedSetDigest(entries)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(digest) != 64 }), "expected-set digest = %q, %v", digest, err)
	group, err := GroupKey("games/title", strings.Repeat("d", 64))
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(group) != 64 }), "group key = %q, %v", group, err)
	allowed := [][2]AttachmentState{
		{AttachmentQueued, AttachmentRunning},
		{AttachmentQueued, AttachmentCancelled},
		{AttachmentRunning, AttachmentAccepted},
		{AttachmentRunning, AttachmentRejected},
		{AttachmentRunning, AttachmentFailedRetryable},
		{AttachmentRunning, AttachmentCancelled},
		{AttachmentFailedRetryable, AttachmentRunning},
		{AttachmentFailedRetryable, AttachmentCancelled},
	}
	for _, transition := range allowed {
		testassert.Truef(t, CanTransitionAttachment(transition[0], transition[1]), "transition %s -> %s rejected", transition[0], transition[1])
	}
	testassert.False(t, testassert.Any(func() bool { return CanTransitionAttachment(AttachmentAccepted, AttachmentRunning) }, func() bool { return CanTransitionAttachment(AttachmentFailedRetryable, AttachmentQueued) }, func() bool { return CanTransitionAttachment("UNKNOWN", AttachmentRunning) }), "invalid attachment transition accepted")
}

func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("one.chd\ntwo.chd\n"),
		append([]byte{0xef, 0xbb, 0xbf}, []byte("ONE.CHD\r\ntwo.chd\r\n")...),
		[]byte("../one.chd\ntwo.chd\n"),
		[]byte("one.chd\nONE.CHD\n"),
		bytes.Repeat([]byte{'x'}, MaxPlaylistBytes+1),
		{0xff, 0xfe, 0xfd},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, playlist []byte) {
		result, _ := Parse(playlist, []File{
			{Basename: "one.chd", SizeBytes: 8, Header: []byte("MComprHD")},
			{Basename: "two.chd", SizeBytes: 8, Header: []byte("MComprHD")},
		}, DefaultLimits())
		testassert.Falsef(t, testassert.Any(func() bool { return len(result.Entries) > MaxDiscs }, func() bool { return len(result.CanonicalPlaylist) > MaxDiscs*13 }), "unbounded result: %#v", result)
	})
}
