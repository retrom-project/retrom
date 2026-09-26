package emulationstationmeta

import (
	"fmt"
	"strings"
	"testing"
)

func TestPlayersNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value       string
		wantPlayers *int
		warningCode string
	}{
		{value: "1", wantPlayers: intPointer(1)},
		{value: "01", wantPlayers: intPointer(1)},
		{value: "64", wantPlayers: intPointer(64)},
		{value: "1-4", wantPlayers: intPointer(4), warningCode: WarningPlayerRange},
		{value: "04-04", wantPlayers: intPointer(4), warningCode: WarningPlayerRange},
		{value: "", wantPlayers: nil},
		{value: "0", wantPlayers: nil, warningCode: WarningFieldInvalid},
		{value: "65", wantPlayers: nil, warningCode: WarningFieldInvalid},
		{value: "+1", wantPlayers: nil, warningCode: WarningFieldInvalid},
		{value: "1+", wantPlayers: nil, warningCode: WarningFieldInvalid},
		{value: "4-1", wantPlayers: nil, warningCode: WarningFieldInvalid},
		{value: "1 - 4", wantPlayers: nil, warningCode: WarningFieldInvalid},
		{value: "１", wantPlayers: nil, warningCode: WarningFieldInvalid},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("value_%q", test.value), func(t *testing.T) {
			t.Parallel()
			game := parseOneGame(t, "<players>"+test.value+"</players>", 2027)
			assertOptionalInt(t, game.Metadata.Players, test.wantPlayers)
			if test.warningCode == "" {
				if warningCount(game.Warnings, WarningFieldInvalid, "players") != 0 ||
					warningCount(game.Warnings, WarningPlayerRange, "players") != 0 {
					t.Fatalf("unexpected players warning: %#v", game.Warnings)
				}
			} else {
				assertWarning(t, game.Warnings, test.warningCode, "players")
			}
		})
	}
}

func TestReleaseDateNormalizationUsesFrozenMaximum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value       string
		maximum     int
		wantYear    *int
		wantWarning bool
	}{
		{value: "20240229T235959", maximum: 2027, wantYear: intPointer(2024)},
		{value: "20240229", maximum: 2027, wantYear: intPointer(2024)},
		{value: "29/02/2024", maximum: 2027, wantYear: intPointer(2024)},
		{value: "", maximum: 2027},
		{value: "19491231", maximum: 2027, wantWarning: true},
		{value: "20280229", maximum: 2027, wantWarning: true},
		{value: "20240230", maximum: 2027, wantWarning: true},
		{value: "31/04/2024", maximum: 2027, wantWarning: true},
		{value: "20240229T246000", maximum: 2027, wantWarning: true},
		{value: "2024-02-29", maximum: 2027, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			game := parseOneGameWithMaximum(t, "<releasedate>"+test.value+"</releasedate>", test.maximum)
			assertOptionalInt(t, game.Metadata.ReleaseYear, test.wantYear)
			gotWarning := warningCount(game.Warnings, WarningFieldInvalid, "releasedate") != 0
			if gotWarning != test.wantWarning {
				t.Fatalf("warnings = %#v, want warning %t", game.Warnings, test.wantWarning)
			}
		})
	}
}

func TestSourceFlagsAreStrictASCIIBooleans(t *testing.T) {
	t.Parallel()
	xmlFields := `<hidden>TrUe</hidden><adult>FALSE</adult><kidgame>yes</kidgame>`
	game := parseOneGame(t, xmlFields, 2027)
	if !game.SourceFlags.Hidden || game.SourceFlags.Adult || game.SourceFlags.KidGame {
		t.Fatalf("flags = %#v", game.SourceFlags)
	}
	assertWarning(t, game.Warnings, WarningFieldInvalid, "kidgame")

	empty := parseOneGame(t, `<hidden/><adult> </adult><kidgame/>`, 2027)
	if empty.SourceFlags.Hidden || empty.SourceFlags.Adult || empty.SourceFlags.KidGame || len(empty.Warnings) != 0 {
		t.Fatalf("empty flags = %#v / %#v", empty.SourceFlags, empty.Warnings)
	}
}

func TestMediaCandidatesKeepFirstDeclarationWithinEachField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		fields     string
		wantMethod string
		wantPath   string
	}{
		{name: "mix", fields: `<mix>mix.png</mix>`, wantMethod: "EXPLICIT_MIX", wantPath: "mix.png"},
		{name: "thumbnail", fields: `<thumbnail>thumb.png</thumbnail>`, wantMethod: "EXPLICIT_THUMBNAIL", wantPath: "thumb.png"},
		{name: "alias", fields: `<thumbnails>thumbs.png</thumbnails>`, wantMethod: "EXPLICIT_THUMBNAIL_ALIAS", wantPath: "thumbs.png"},
		{name: "invalid higher priority", fields: `<image> ../escape.png</image><boxart>safe.png</boxart>`, wantMethod: "EXPLICIT_IMAGE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := parseOneGame(t, test.fields, 2027)
			if game.Assets.Cover == nil || game.Assets.Cover.ResolutionMethod != test.wantMethod ||
				game.Assets.Cover.RelativePath != test.wantPath {
				t.Fatalf("cover = %#v", game.Assets.Cover)
			}
		})
	}
}

func TestIgnoredFieldsAndDuplicateSingletons(t *testing.T) {
	t.Parallel()
	fields := `<rating>1</rating><sortname>a</sortname><family>a</family><lang>en</lang><region>us</region>` +
		`<genreid>1</genreid><arcadesystemname>x</arcadesystemname><playcount>1</playcount>` +
		`<lastplayed>x</lastplayed><gametime>1</gametime><md5>x</md5><crc32>x</crc32><hash>x</hash>` +
		`<cheevosHash>x</cheevosHash><cheevosId>x</cheevosId><manual>x</manual><genre>first</genre><genre>second</genre>`
	game := parseOneGame(t, fields, 2027)
	if game.Metadata.Genre != "first" {
		t.Fatalf("genre = %q", game.Metadata.Genre)
	}
	assertWarning(t, game.Warnings, WarningDuplicateField, "genre")
	for _, field := range []string{
		"rating", "sortname", "family", "lang", "region", "genreid", "arcadesystemname", "playcount",
		"lastplayed", "gametime", "md5", "crc32", "hash", "cheevosHash", "cheevosId", "manual",
	} {
		assertWarning(t, game.Warnings, WarningFieldIgnored, field)
	}
}

func parseOneGame(t *testing.T, fields string, releaseYearMax int) Game {
	t.Helper()
	return parseOneGameWithMaximum(t, fields, releaseYearMax)
}

func parseOneGameWithMaximum(t *testing.T, fields string, releaseYearMax int) Game {
	t.Helper()
	document, err := Parse([]byte(`<gameList><game><path>safe.gba</path>`+fields+`</game></gameList>`), releaseYearMax)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(document.Games) != 1 {
		t.Fatalf("games = %#v", document.Games)
	}
	return document.Games[0]
}

func intPointer(value int) *int {
	return &value
}

func assertOptionalInt(t *testing.T, actual, expected *int) {
	t.Helper()
	if actual == nil || expected == nil {
		if actual != expected {
			t.Fatalf("value = %v, want %v", actual, expected)
		}
		return
	}
	if *actual != *expected {
		t.Fatalf("value = %d, want %d", *actual, *expected)
	}
}

func TestTextFieldsUseFirstNodeEvenWhenEmpty(t *testing.T) {
	t.Parallel()
	game := parseOneGame(t, `<name/><name>ignored</name><developer> first </developer><developer>ignored</developer>`, 2027)
	if game.Metadata.Title != "safe" || game.Metadata.Developer != "first" {
		t.Fatalf("metadata = %#v", game.Metadata)
	}
	for _, field := range []string{"name", "developer"} {
		assertWarning(t, game.Warnings, WarningDuplicateField, field)
	}
}

func TestDescriptionKeepsInternalWhitespaceAfterTrim(t *testing.T) {
	t.Parallel()
	game := parseOneGame(t, "<desc>\n  line one\n\nline two\t\n</desc>", 2027)
	if game.Metadata.Description != "line one\n\nline two" {
		t.Fatalf("description = %q", game.Metadata.Description)
	}
}

func TestDateParserDoesNotAcceptWhitespaceInsideValue(t *testing.T) {
	t.Parallel()
	game := parseOneGame(t, `<releasedate> 20240229 </releasedate>`, 2027)
	if game.Metadata.ReleaseYear == nil || *game.Metadata.ReleaseYear != 2024 {
		t.Fatalf("release year = %v, warnings=%#v", game.Metadata.ReleaseYear, game.Warnings)
	}
}

func TestReleaseDateFieldDoesNotUseCurrentClock(t *testing.T) {
	t.Parallel()
	game := parseOneGame(t, `<releasedate>20270101</releasedate>`, 2026)
	if game.Metadata.ReleaseYear != nil || warningCount(game.Warnings, WarningFieldInvalid, "releasedate") != 1 {
		t.Fatalf("metadata = %#v, warnings=%#v", game.Metadata, game.Warnings)
	}
}

func TestPathValueIsNotTrimmedLikeMetadata(t *testing.T) {
	t.Parallel()
	for _, value := range []string{" safe.gba", "safe.gba ", "\u00a0safe.gba", "safe.gba\u00a0"} {
		t.Run(strings.ReplaceAll(value, " ", "_"), func(t *testing.T) {
			t.Parallel()
			xmlDocument := `<gameList><game><path>` + value + `</path></game></gameList>`
			document, err := Parse([]byte(xmlDocument), 2027)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if document.Games[0].BlockedCode != CodePathInvalid {
				t.Fatalf("path %q projection = %#v", value, document.Games[0])
			}
		})
	}
}
