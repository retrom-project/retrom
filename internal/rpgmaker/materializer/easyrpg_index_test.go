package materializer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestBuildEasyRPGIndexMatchesGencacheV2LookupShape(t *testing.T) {
	index, err := BuildEasyRPGIndex([]SourceFile{
		{Path: "Picture/Title.PNG"},
		{Path: "RPG_RT.ldb"},
		{Path: "Picture/Translation.PO"},
		{Path: "ExFont.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"cache":{"exfont":"ExFont.png","picture":{"_dirname":"Picture","title":"Title.PNG","translation.po":"Translation.PO"},"rpg_rt.ldb":"RPG_RT.ldb"},"metadata":{"version":2}}`
	if string(index.Contents) != want {
		t.Fatalf("index=%s\nwant=%s", index.Contents, want)
	}
	digest := sha256.Sum256([]byte(want))
	if index.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("digest=%s", index.SHA256)
	}
}

func TestBuildEasyRPGIndexRejectsRuntimeLookupCollisions(t *testing.T) {
	tests := map[string][]SourceFile{
		"extension stripped": {{Path: "Picture/Hero.png"}, {Path: "Picture/Hero.xyz"}},
		"nfkc directory":     {{Path: "Ｋ/Hero.png"}, {Path: "k/Other.png"}},
		"reserved key":       {{Path: "_dirname/File.png"}},
		"file directory":     {{Path: "Picture"}, {Path: "Picture/Hero.png"}},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildEasyRPGIndex(files); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error=%v, want %v", err, ErrInvalid)
			}
		})
	}
}
