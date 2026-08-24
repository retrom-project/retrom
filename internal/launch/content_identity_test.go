package launch

import (
	"strings"
	"testing"
)

func TestContentIdentityUsesBytesAndDOSProjection(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	content := ContentView{Digest: digest, Format: "RETROM_SINGLE_FILE_V1", CoreID: "mgba"}
	identity, err := ContentIdentity(content)
	if err != nil || identity == digest {
		t.Fatalf("single identity = %q, error=%v", identity, err)
	}
	repeated, err := ContentIdentity(content)
	if err != nil || repeated != identity {
		t.Fatalf("repeated identity = %q, error=%v; want %q", repeated, err, identity)
	}
	replacedDigest := strings.Repeat("b", 64)
	replacedIdentity, err := ContentIdentity(ContentView{
		Digest: replacedDigest, Format: "RETROM_SINGLE_FILE_V1", CoreID: "mgba",
	})
	if err != nil || replacedIdentity == identity {
		t.Fatalf("replaced identity = %q, error=%v; original=%q", replacedIdentity, err, identity)
	}
	entryA := "GAMEA.EXE"
	entryB := "GAMEB.EXE"
	dosA, err := ContentIdentity(ContentView{
		Digest: digest, Format: "RETROM_DOS_DIRECT_ZIP_V1", CoreID: "dosbox_pure", DOSEntry: &entryA,
	})
	if err != nil {
		t.Fatal(err)
	}
	dosB, err := ContentIdentity(ContentView{
		Digest: digest, Format: "RETROM_DOS_DIRECT_ZIP_V1", CoreID: "dosbox_pure", DOSEntry: &entryB,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dosA == digest || dosA == dosB {
		t.Fatalf("DOS identities did not bind output selection: raw=%s A=%s B=%s", digest, dosA, dosB)
	}
	externalIdentity, err := ExternalContentIdentity(digest)
	if err != nil || externalIdentity == digest || externalIdentity == identity {
		t.Fatalf("external identity = %q, error=%v; blob=%q game=%q", externalIdentity, err, digest, identity)
	}
}

func TestBundleIdentityIsOrderIndependentAndBindsEveryMember(t *testing.T) {
	t.Parallel()
	files := []BundleFile{
		{LogicalName: "bios-b.bin", SHA256: strings.Repeat("b", 64)},
		{LogicalName: "bios-a.bin", SHA256: strings.Repeat("a", 64)},
	}
	identity, err := BundleIdentity(files)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := BundleIdentity([]BundleFile{files[1], files[0]})
	if err != nil || reordered != identity {
		t.Fatalf("reordered identity = %q, error=%v; want %q", reordered, err, identity)
	}
	replaced, err := BundleIdentity([]BundleFile{
		{LogicalName: "bios-a.bin", SHA256: strings.Repeat("a", 64)},
		{LogicalName: "bios-b.bin", SHA256: strings.Repeat("c", 64)},
	})
	if err != nil || replaced == identity {
		t.Fatalf("replacement identity = %q, error=%v; original=%q", replaced, err, identity)
	}
}

func TestRuntimeContentURLRejectsUnsafeOrNonCanonicalInputs(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	identity, err := ContentIdentity(ContentView{
		Digest: digest, Format: "RETROM_SINGLE_FILE_V1", CoreID: "mgba",
	})
	if err != nil {
		t.Fatal(err)
	}
	contentURL, err := RuntimeContentURL("game", identity, "Game One (USA).zip")
	if err != nil || contentURL != "/runtime/content/game/"+identity+"/Game%20One%20%28USA%29.zip" {
		t.Fatalf("content URL = %q, error=%v", contentURL, err)
	}
	for _, input := range []struct {
		kind, identity, name string
	}{
		{"save", digest, "game.zip"},
		{"game", strings.Repeat("A", 64), "game.zip"},
		{"game", digest, "../game.zip"},
		{"game", digest, `dir\game.zip`},
	} {
		if _, err := RuntimeContentURL(input.kind, input.identity, input.name); err == nil {
			t.Fatalf("RuntimeContentURL(%q, %q, %q) accepted", input.kind, input.identity, input.name)
		}
	}
}
