package pegasusmeta

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestParseProjectsFieldsAliasesAndFlowingText(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("\xef\xbb\xbfCoLlEcTiOn: FC\r\nshortname: famicom\r\nextensions: zip\r\nassets.boxfront: media/default.png\r\ngame: Metal Max\r\ndescription: Wasteland\r\n .\r\n Tank\\ncombat\r\ndevelopers: Crea-Tech\r\n Data East\r\npublishers: Data East\r\ngenres: RPG\r\nplayers: 1\r\nrelease: 1991-05-24\r\nfiles: roms\\Metal Max.zip\r\nassets.box_front: media/Metal Max/boxFront.png\r\nassets.video: media/Metal Max/video.mp4\r\nassets.logo: ignored.png\r\nlaunch: secret command\r\n"))
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return len(document.Collections) != 1 }, func() bool { return len(document.Collections[0].Games) != 1 }), "projection = %#v", document)
	collection := document.Collections[0]
	game := collection.Games[0]
	testassert.Falsef(t, testassert.Any(func() bool { return collection.Name != "FC" }, func() bool { return collection.ShortName != "famicom" }, func() bool { return len(collection.IgnoredRules) != 1 }, func() bool { return game.Metadata.Title != "Metal Max" }, func() bool { return game.Metadata.Description != "Wasteland\n\nTank\ncombat" }, func() bool { return game.Metadata.Developer != "Crea-Tech / Data East" }, func() bool { return game.Metadata.Publisher != "Data East" }, func() bool { return game.Metadata.Genre != "RPG" }, func() bool { return game.Metadata.Players == nil }, func() bool { return *game.Metadata.Players != 1 }, func() bool { return game.Metadata.ReleaseYear == nil }, func() bool { return *game.Metadata.ReleaseYear != 1991 }, func() bool { return len(game.Files) != 1 }, func() bool { return game.Files[0] != `roms\Metal Max.zip` }, func() bool { return len(game.Assets.Covers) != 1 }, func() bool { return len(game.Assets.Videos) != 1 }, func() bool { return game.BlockedCode != "" }), "collection/game = %#v / %#v", collection, game)
	for _, field := range game.UnknownFields {
		testassert.Falsef(t, testassert.Any(func() bool { return field == "launch" }, func() bool { return field == "assets.logo" }), "recognized ignored field leaked as unknown: %q", field)
	}
}

func TestParseKeepsSegmentsAndBlocksOrphansAndMissingFiles(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("game: orphan\nfile: orphan.nes\ncollection: Same\ngame: first\ncollection: Same\ngame: second\nfile: second.nes\n"))
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return len(document.OrphanGames) != 1 }, func() bool { return document.OrphanGames[0].BlockedCode != "PEGASUS_GAME_WITHOUT_COLLECTION" }), "orphans = %#v", document.OrphanGames)
	testassert.Falsef(t, testassert.Any(func() bool { return len(document.Collections) != 2 }, func() bool { return document.Collections[0].SegmentOrdinal != 0 }, func() bool { return document.Collections[1].SegmentOrdinal != 1 }, func() bool { return document.Collections[0].Games[0].BlockedCode != "PEGASUS_GAME_WITHOUT_FILE" }), "collections = %#v", document.Collections)
}

func TestParseWarningsAndBounds(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("collection: Test\ngame: current\nplayers: 1\nplayers: 1-4\nrelease: 2024-13-01\ndescription: " + strings.Repeat("界", MaxDescriptionRunes+1) + "\nfile: game.rom\n"))
	testassert.False(t, err != nil, err)
	game := document.Collections[0].Games[0]
	testassert.Falsef(t, testassert.Any(func() bool { return game.Metadata.Title != "current" }, func() bool { return game.Metadata.Players != nil }, func() bool { return game.Metadata.ReleaseYear != nil }, func() bool { return len([]rune(game.Metadata.Description)) != MaxDescriptionRunes }), "game = %#v", game)
	codes := map[string]bool{}
	for _, warning := range game.Warnings {
		codes[warning.Code+":"+warning.Field] = true
	}
	for _, expected := range []string{"DUPLICATE_SINGLETON_FIELD:players", "FIELD_VALUE_INVALID:players", "FIELD_VALUE_INVALID:release", "FIELD_TRUNCATED:description"} {
		testassert.Truef(t, codes[expected], "missing warning %s in %#v", expected, game.Warnings)
	}
}

func TestParseRejectsWholeMetadataOnSyntaxEncodingAndLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		contents []byte
		want     error
	}{
		{name: "continuation", contents: []byte(" continuation"), want: ErrSyntax},
		{name: "missing colon", contents: []byte("collection"), want: ErrSyntax},
		{name: "empty key", contents: []byte(": value"), want: ErrSyntax},
		{name: "control", contents: []byte("collection: bad\x00value"), want: ErrSyntax},
		{name: "late bom", contents: []byte("collection: x\n\xef\xbb\xbfgame: y"), want: ErrInvalidUTF8},
		{name: "invalid utf8", contents: []byte{0xff}, want: ErrInvalidUTF8},
		{name: "long line", contents: []byte("collection: " + strings.Repeat("x", MaxPhysicalLine)), want: ErrSyntax},
		{name: "large file", contents: make([]byte, MaxMetadataBytes+1), want: ErrTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(test.contents)
			testassert.Truef(t, errors.Is(err, test.want), "error = %v, want %v", err, test.want)
		})
	}
}
