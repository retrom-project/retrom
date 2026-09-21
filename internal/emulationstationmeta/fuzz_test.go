package emulationstationmeta

import "testing"

func FuzzParse(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`<gameList/>`),
		[]byte(`<?xml version="1.0"?><gameList><game><path>./safe.gba</path><name>Safe</name></game></gameList>`),
		[]byte(`<gameList><game><path>one.gba</path><path>two.gba</path><command>ignored</command></game></gameList>`),
		[]byte(`<!DOCTYPE gameList [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><gameList>&xxe;</gameList>`),
		{0xef, 0xbb, 0xbf, '<', 'g', 'a', 'm', 'e', 'L', 'i', 's', 't', '/', '>'},
		{0xff},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, contents []byte) {
		document, err := Parse(contents, 2027)
		if err != nil {
			return
		}
		if len(document.Games) > MaxGames || len(document.IgnoredFields) > MaxIgnoredFieldNames {
			t.Fatalf("projection exceeded document limits: %#v", document)
		}
		for _, game := range document.Games {
			if len(game.Warnings) > MaxWarnings || game.Ordinal < 1 || game.Ordinal > len(document.Games) {
				t.Fatalf("projection exceeded item limits: %#v", game)
			}
			if len([]rune(game.Metadata.Title)) > MaxTitleRunes ||
				len([]rune(game.Metadata.Description)) > MaxDescriptionRunes ||
				len([]rune(game.Metadata.Developer)) > MaxMetadataRunes ||
				len([]rune(game.Metadata.Publisher)) > MaxMetadataRunes ||
				len([]rune(game.Metadata.Genre)) > MaxMetadataRunes {
				t.Fatalf("projection exceeded text limits: %#v", game.Metadata)
			}
		}
	})
}

func FuzzNormalizeDeclaredPath(f *testing.F) {
	for _, seed := range []string{
		"./safe.gba", `nested\safe.gba`, "../escape", "/absolute", "https://example.invalid/game",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, declared string) {
		normalized, err := NormalizeDeclaredPath(declared)
		if err != nil {
			if normalized != "" {
				t.Fatalf("invalid path retained a projection: %q", normalized)
			}
			return
		}
		if normalized == "" || len(normalized) > MaxDeclaredPathBytes {
			t.Fatalf("invalid normalized path: %q", normalized)
		}
	})
}
