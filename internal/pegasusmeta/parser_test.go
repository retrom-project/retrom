package pegasusmeta

import (
	"errors"
	"strings"
	"testing"
)

func TestParseProjectsFieldsAliasesAndFlowingText(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("\xef\xbb\xbfCoLlEcTiOn: FC\r\nshortname: famicom\r\nextensions: zip\r\nassets.boxfront: media/default.png\r\ngame: Metal Max\r\ndescription: Wasteland\r\n .\r\n Tank\\ncombat\r\ndevelopers: Crea-Tech\r\n Data East\r\npublishers: Data East\r\ngenres: RPG\r\nplayers: 1\r\nrelease: 1991-05-24\r\nfiles: roms\\Metal Max.zip\r\nassets.box_front: media/Metal Max/boxFront.png\r\nassets.video: media/Metal Max/video.mp4\r\nassets.logo: ignored.png\r\nlaunch: secret command\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Collections) != 1 || len(document.Collections[0].Games) != 1 {
		t.Fatalf("projection = %#v", document)
	}
	collection := document.Collections[0]
	game := collection.Games[0]
	if collection.Name != "FC" || collection.ShortName != "famicom" || len(collection.IgnoredRules) != 1 ||
		game.Metadata.Title != "Metal Max" || game.Metadata.Description != "Wasteland\n\nTank\ncombat" ||
		game.Metadata.Developer != "Crea-Tech / Data East" || game.Metadata.Publisher != "Data East" ||
		game.Metadata.Genre != "RPG" || game.Metadata.Players == nil || *game.Metadata.Players != 1 ||
		game.Metadata.ReleaseYear == nil || *game.Metadata.ReleaseYear != 1991 ||
		len(game.Files) != 1 || game.Files[0] != `roms\Metal Max.zip` ||
		len(game.Assets.Covers) != 1 || len(game.Assets.Videos) != 1 || game.BlockedCode != "" {
		t.Fatalf("collection/game = %#v / %#v", collection, game)
	}
	for _, field := range game.UnknownFields {
		if field == "launch" || field == "assets.logo" {
			t.Fatalf("recognized ignored field leaked as unknown: %q", field)
		}
	}
}

func TestParseKeepsSegmentsAndBlocksOrphansAndMissingFiles(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("game: orphan\nfile: orphan.nes\ncollection: Same\ngame: first\ncollection: Same\ngame: second\nfile: second.nes\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.OrphanGames) != 1 || document.OrphanGames[0].BlockedCode != "PEGASUS_GAME_WITHOUT_COLLECTION" {
		t.Fatalf("orphans = %#v", document.OrphanGames)
	}
	if len(document.Collections) != 2 || document.Collections[0].SegmentOrdinal != 0 ||
		document.Collections[1].SegmentOrdinal != 1 || document.Collections[0].Games[0].BlockedCode != "PEGASUS_GAME_WITHOUT_FILE" {
		t.Fatalf("collections = %#v", document.Collections)
	}
}

func TestParseWarningsAndBounds(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("collection: Test\ngame: current\nplayers: 1\nplayers: 1-4\nrelease: 2024-13-01\ndescription: " + strings.Repeat("界", MaxDescriptionRunes+1) + "\nfile: game.rom\n"))
	if err != nil {
		t.Fatal(err)
	}
	game := document.Collections[0].Games[0]
	if game.Metadata.Title != "current" || game.Metadata.Players != nil || game.Metadata.ReleaseYear != nil ||
		len([]rune(game.Metadata.Description)) != MaxDescriptionRunes {
		t.Fatalf("game = %#v", game)
	}
	codes := map[string]bool{}
	for _, warning := range game.Warnings {
		codes[warning.Code+":"+warning.Field] = true
	}
	for _, expected := range []string{"DUPLICATE_SINGLETON_FIELD:players", "FIELD_VALUE_INVALID:players", "FIELD_VALUE_INVALID:release", "FIELD_TRUNCATED:description"} {
		if !codes[expected] {
			t.Fatalf("missing warning %s in %#v", expected, game.Warnings)
		}
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
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
