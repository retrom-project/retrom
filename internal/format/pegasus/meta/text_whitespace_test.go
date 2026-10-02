package pegasusmeta

import (
	"errors"
	"strings"
	"testing"
)

func TestTextTabsAndContinuationPreserveEveryGame(t *testing.T) {
	t.Parallel()
	input := "collection: Demo\r\ndescription: collection\ttext\r\n" +
		"game: First\r\nfile: first.nes\r\ndescription: text\ttext\r\n" +
		"\tcontinued\ttext\r\n .\r\n last paragraph\r\n" +
		"game: Second\r\nfile: second.nes\r\ndescription: another\tvalue\r\n"
	document, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	collection := document.Collections[0]
	if len(collection.Games) != 2 || collection.Description != "collection\ttext" {
		t.Fatalf("lost declarations: %#v", collection)
	}
	if got := collection.Games[0].Metadata.Description; got != "text\ttext continued\ttext\n\nlast paragraph" {
		t.Fatalf("description: %q", got)
	}
	if collection.Games[1].Metadata.Description != "another\tvalue" {
		t.Fatal("second game text lost")
	}
}

func TestTextWhitespaceDoesNotRelaxTitleOrControlValidation(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("collection: Demo\ngame: bad\ttitle\nfile: first.nes\ngame: Valid\nfile: second.nes\n"))
	if err != nil {
		t.Fatal(err)
	}
	games := document.Collections[0].Games
	if games[0].BlockedCode != "PEGASUS_GAME_TITLE_INVALID" || games[1].BlockedCode != "" {
		t.Fatalf("field validation: %#v", games)
	}
	for _, control := range []string{"\x00", "\x01", "\x7f", "\u0085"} {
		_, err := Parse([]byte("collection: Demo\ngame: Example\ndescription: text" + control + "text\n"))
		if !errors.Is(err, ErrSyntax) || !strings.Contains(err.Error(), "line 3") {
			t.Fatalf("control %q error: %v", control, err)
		}
	}
}
